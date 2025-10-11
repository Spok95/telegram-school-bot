package app

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/observability"
)

func TryHandleTeacherAddSlots(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" {
		return false
	}

	txt := strings.TrimSpace(msg.Text)
	parts := splitArgs(txt)
	if len(parts) == 0 {
		return false
	}
	// принимаем /t_addslots и /t_addslots@BotName
	if !strings.HasPrefix(parts[0], "/t_addslots") {
		return false
	}

	chatID := msg.Chat.ID

	// Проверка роли: только учитель
	u, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Teacher {
		reply(bot, chatID, "Эта команда доступна только учителям.")
		return true
	}

	// ожидаем: /t_addslots <день> <HH:MM-HH:MM> <шаг-мин> <class_id>
	if len(parts) < 5 {
		reply(bot, chatID, usageAddSlots())
		return true
	}
	dayWord := parts[1]
	timeWin := parts[2]
	stepStr := parts[3]
	classStr := parts[4]

	weekday, ok := parseWeekday(dayWord)
	if !ok {
		reply(bot, chatID, "Неверный день недели. Используй: пн..вс или mon..sun.")
		return true
	}

	startT, endT, ok := parseTimeWindow(timeWin)
	if !ok {
		reply(bot, chatID, "Неверный формат времени. Ожидаю HH:MM-HH:MM, пример: 16:00-18:00.")
		return true
	}
	if !startT.Before(endT) {
		reply(bot, chatID, "start должен быть раньше end.")
		return true
	}

	stepMin, err := strconv.Atoi(stepStr)
	if err != nil || stepMin <= 0 {
		reply(bot, chatID, "Шаг должен быть положительным числом минут.")
		return true
	}
	classID, err := strconv.ParseInt(classStr, 10, 64)
	if err != nil || classID <= 0 {
		reply(bot, chatID, "class_id должен быть положительным числом.")
		return true
	}

	// Генерация стартов слотов на 4 недели вперёд
	loc := time.Local // при желании подставим таймзону школы из конфига
	starts := generateStartsWeeks(weekday, startT, endT, time.Duration(stepMin)*time.Minute, 1, loc)

	// Конвертируем локальные времена в UTC для хранения
	startsUTC := make([]time.Time, 0, len(starts))
	for _, lt := range starts {
		startsUTC = append(startsUTC, lt.UTC())
	}

	inserted, err := db.CreateSlots(ctx, database, u.ID, classID, startsUTC, time.Duration(stepMin)*time.Minute)
	if err != nil {
		observability.CaptureErr(err)
		reply(bot, chatID, "Ошибка при создании слотов.")
		return true
	}

	reply(bot, chatID, fmt.Sprintf("Готово. Создано слотов: %d (дубликаты проигнорированы).", inserted))
	return true
}

// --- helpers ---

func usageAddSlots() string {
	return "Использование:\n" +
		"/t_addslots <день> <HH:MM-HH:MM> <шаг-мин> <class_id>\n" +
		"Примеры:\n" +
		"  /t_addslots пн 16:00-18:00 20 7\n" +
		"  /t_addslots wed 09:00-11:30 30 12"
}

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) {
	_, _ = bot.Send(tgbotapi.NewMessage(chatID, text))
}

func splitArgs(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) })
}

func parseWeekday(w string) (time.Weekday, bool) {
	w = strings.ToLower(strings.TrimSpace(w))
	switch w {
	case "mon", "monday", "пн", "пон", "понедельник":
		return time.Monday, true
	case "tue", "tuesday", "вт", "вторник":
		return time.Tuesday, true
	case "wed", "wednesday", "ср", "среда":
		return time.Wednesday, true
	case "thu", "thursday", "чт", "четверг":
		return time.Thursday, true
	case "fri", "friday", "пт", "пятница":
		return time.Friday, true
	case "sat", "saturday", "сб", "суббота":
		return time.Saturday, true
	case "sun", "sunday", "вс", "воскресенье":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func parseTimeWindow(s string) (time.Time, time.Time, bool) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, false
	}
	parse := func(p string) (time.Time, bool) {
		p = strings.TrimSpace(p)
		t, err := time.Parse("15:04", p)
		if err != nil {
			return time.Time{}, false
		}
		// фиктивная дата (важны только часы:минуты)
		return time.Date(2000, 1, 1, t.Hour(), t.Minute(), 0, 0, time.Local), true
	}
	st, ok1 := parse(parts[0])
	en, ok2 := parse(parts[1])
	return st, en, ok1 && ok2
}

func nextWeekday(base time.Time, wd time.Weekday) time.Time {
	offset := (int(wd) - int(base.Weekday()) + 7) % 7
	return base.AddDate(0, 0, offset)
}

// было generateStartsNext4Weeks — делаем универсальный
func generateStartsWeeks(wd time.Weekday, startT, endT time.Time, step time.Duration, weeks int, loc *time.Location) []time.Time {
	now := time.Now().In(loc)
	firstDay := nextWeekday(now, wd)
	makeDayTime := func(day time.Time, hh, mm int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
	}
	if step <= 0 {
		return nil
	}
	var starts []time.Time
	for week := 0; week < weeks; week++ {
		day := firstDay.AddDate(0, 0, 7*week)
		startBound := makeDayTime(day, startT.Hour(), startT.Minute())
		endBound := makeDayTime(day, endT.Hour(), endT.Minute())
		for tm := startBound; !tm.Add(step).After(endBound); tm = tm.Add(step) {
			starts = append(starts, tm)
		}
	}
	return starts
}
