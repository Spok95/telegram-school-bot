package app

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

// ====== STATE ======

type teacherFSMState struct {
	Step    int
	Weekday time.Weekday
	Start   time.Time // фиктивная дата, время важно
	End     time.Time
	StepMin int
	ClassID int64
}

var teacherFSM sync.Map // key: chatID(int64) -> *teacherFSMState

func getTeacherFSM(chatID int64) (*teacherFSMState, bool) {
	v, ok := teacherFSM.Load(chatID)
	if !ok {
		return nil, false
	}
	return v.(*teacherFSMState), true
}
func setTeacherFSM(chatID int64, st *teacherFSMState) { teacherFSM.Store(chatID, st) }
func clearTeacherFSM(chatID int64)                    { teacherFSM.Delete(chatID) }

// ====== ENTRY POINTS ======

// Команда запуска FSM
func TryHandleTeacherSlotsCommand(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" {
		return false
	}
	if !strings.HasPrefix(msg.Text, "/t_slots") {
		return false
	}
	// проверка роли: только учитель
	u, _ := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if u == nil || u.Role == nil || *u.Role != models.Teacher {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Эта команда доступна только учителям."))
		return true
	}
	// сбрасываем состояние и показываем выбор дня недели
	clearTeacherFSM(msg.Chat.ID)
	setTeacherFSM(msg.Chat.ID, &teacherFSMState{Step: 1})
	sendWeekdayMenu(bot, msg.Chat.ID)
	return true
}

// Обработка нажатий кнопок FSM учителя
func TryHandleTeacherSlotsCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" {
		return false
	}
	if !strings.HasPrefix(cb.Data, "t_slots:") {
		return false
	}
	parts := strings.Split(cb.Data, ":")
	// варианты:
	// t_slots:day:<0..6>
	// t_slots:cancel
	switch parts[1] {
	case "cancel":
		clearTeacherFSM(cb.Message.Chat.ID)
		editText(bot, cb, "Отменено.")
		return true
	case "day":
		if len(parts) < 3 {
			answer(bot, cb, "Ошибка данных.")
			return true
		}
		wdNum, _ := strconv.Atoi(parts[2])
		st, ok := getTeacherFSM(cb.Message.Chat.ID)
		if !ok {
			setTeacherFSM(cb.Message.Chat.ID, &teacherFSMState{Step: 1})
			st, _ = getTeacherFSM(cb.Message.Chat.ID)
		}
		st.Weekday = time.Weekday(wdNum)
		st.Step = 2
		setTeacherFSM(cb.Message.Chat.ID, st)
		editText(bot, cb, "Шаг 2/4. Введите временное окно в формате HH:MM-HH:MM (например, 16:00-18:00)")
		return true
	default:
		return false
	}
}

// Обработка текстовых шагов FSM учителя (после выбора дня)
func TryHandleTeacherSlotsText(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	st, ok := getTeacherFSM(msg.Chat.ID)
	if !ok {
		return false
	}
	switch st.Step {
	case 2: // ждём HH:MM-HH:MM
		startT, endT, ok := parseTimeWindow(strings.TrimSpace(msg.Text))
		if !ok || !startT.Before(endT) {
			_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Неверный формат. Пример: 16:00-18:00"))
			return true
		}
		st.Start, st.End = startT, endT
		st.Step = 3
		setTeacherFSM(msg.Chat.ID, st)
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Шаг 3/4. Введите шаг в минутах (например, 20)"))
		return true
	case 3: // ждём шаг
		stepMin, err := strconv.Atoi(strings.TrimSpace(msg.Text))
		if err != nil || stepMin <= 0 {
			_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Шаг должен быть положительным числом минут."))
			return true
		}
		st.StepMin = stepMin
		st.Step = 4
		setTeacherFSM(msg.Chat.ID, st)
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Шаг 4/4. Введите class_id (число)."))
		return true
	case 4: // ждём class_id → генерим слоты
		classID, err := strconv.ParseInt(strings.TrimSpace(msg.Text), 10, 64)
		if err != nil || classID <= 0 {
			_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "class_id должен быть положительным числом."))
			return true
		}
		st.ClassID = classID
		// генерация
		loc := time.Local
		starts := generateStartsNext4Weeks(st.Weekday, st.Start, st.End, time.Duration(st.StepMin)*time.Minute, loc)
		if len(starts) == 0 {
			_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Окно времени пустое — слоты не созданы."))
			clearTeacherFSM(msg.Chat.ID)
			return true
		}
		utc := make([]time.Time, 0, len(starts))
		for _, lt := range starts {
			utc = append(utc, lt.UTC())
		}

		u, _ := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
		inserted, err := db.CreateSlots(ctx, database, u.ID, st.ClassID, utc, time.Duration(st.StepMin)*time.Minute)
		if err != nil {
			_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при создании слотов."))
			clearTeacherFSM(msg.Chat.ID)
			return true
		}
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Готово. Создано слотов: %d (дубли проигнорированы).", inserted)))
		clearTeacherFSM(msg.Chat.ID)
		return true
	default:
		return false
	}
}

// ====== UI helpers ======

func sendWeekdayMenu(bot *tgbotapi.BotAPI, chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		row(wdBtn("Пн", time.Monday), wdBtn("Вт", time.Tuesday), wdBtn("Ср", time.Wednesday)),
		row(wdBtn("Чт", time.Thursday), wdBtn("Пт", time.Friday), wdBtn("Сб", time.Saturday)),
		row(wdBtn("Вс", time.Sunday), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
	)
	msg := tgbotapi.NewMessage(chatID, "Шаг 1/4. Выберите день недели:")
	msg.ReplyMarkup = kb
	_, _ = bot.Send(msg)
}
func wdBtn(title string, wd time.Weekday) tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("t_slots:day:%d", int(wd)))
}
func row(btns ...tgbotapi.InlineKeyboardButton) tgbotapi.InlineKeyboardRow {
	return tgbotapi.NewInlineKeyboardRow(btns...)
}
func editText(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, text string) {
	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, text)
	_, _ = bot.Send(edit)
}
func answer(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, text string) {
	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, text))
}
