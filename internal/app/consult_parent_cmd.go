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

func TryHandleParentCommands(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" {
		return false
	}
	parts := fields(msg.Text)
	if len(parts) == 0 {
		return false
	}

	switch {
	// /p_free <teacher_id> <YYYY-MM-DD>
	case strings.HasPrefix(parts[0], "/p_free"):
		return handleParentFree(ctx, bot, database, msg.Chat.ID, parts)
	// /p_book <slot_id>
	case strings.HasPrefix(parts[0], "/p_book"):
		return handleParentBook(ctx, bot, database, msg.Chat.ID, parts)
	default:
		return false
	}
}

func handleParentFree(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, parts []string) bool {
	u, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil {
		observability.CaptureErr(err)
		reply(bot, chatID, "Ошибка профиля пользователя.")
		return true
	}
	if u == nil || u.Role == nil || *u.Role != models.Parent {
		reply(bot, chatID, "Команда доступна только родителям.")
		return true
	}

	if len(parts) < 3 {
		reply(bot, chatID, "Использование: /p_free <teacher_id> <YYYY-MM-DD>")
		return true
	}
	teacherID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || teacherID <= 0 {
		reply(bot, chatID, "teacher_id должен быть положительным числом.")
		return true
	}
	day, err := time.Parse("2006-01-02", parts[2])
	if err != nil {
		reply(bot, chatID, "Дата в формате YYYY-MM-DD.")
		return true
	}

	loc := time.Local
	free, err := db.ListFreeSlotsByTeacherOnDate(ctx, database, teacherID, day, loc, 100)
	if err != nil {
		observability.CaptureErr(err)
		reply(bot, chatID, "Ошибка при получении слотов.")
		return true
	}
	if len(free) == 0 {
		reply(bot, chatID, "На выбранный день свободных слотов нет.")
		return true
	}

	var b strings.Builder
	b.WriteString("Свободные слоты (используй /p_book <slot_id>):\n")
	for _, s := range free {
		start := s.StartAt.In(loc).Format("15:04")
		end := s.EndAt.In(loc).Format("15:04")
		_, _ = fmt.Fprintf(&b, "• #%d %s–%s (class_id=%d)\n", s.ID, start, end, s.ClassID)
	}
	reply(bot, chatID, b.String())
	return true
}

func handleParentBook(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, parts []string) bool {
	u, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil {
		observability.CaptureErr(err)
		reply(bot, chatID, "Ошибка профиля пользователя.")
		return true
	}
	if u == nil || u.Role == nil || *u.Role != models.Parent {
		reply(bot, chatID, "Команда доступна только родителям.")
		return true
	}

	if len(parts) < 2 {
		reply(bot, chatID, "Использование: /p_book <slot_id>")
		return true
	}
	slotID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || slotID <= 0 {
		reply(bot, chatID, "slot_id должен быть положительным числом.")
		return true
	}

	slot, err := db.GetSlotByID(ctx, database, slotID)
	if err != nil {
		observability.CaptureErr(err)
		reply(bot, chatID, "Ошибка чтения слота.")
		return true
	}
	if slot == nil || slot.BookedByID.Valid {
		reply(bot, chatID, "Слот не найден или уже занят.")
		return true
	}

	// Разрешаем запись только к учителю, который ведёт класс ребёнка
	if u.ClassID == nil || *u.ClassID != slot.ClassID {
		reply(bot, chatID, "Нельзя записаться: учитель не привязан к классу вашего ребёнка.")
		return true
	}

	ok, err := db.TryBookSlot(ctx, database, slotID, u.ID)
	if err != nil {
		observability.CaptureErr(err)
		reply(bot, chatID, "Ошибка при бронировании.")
		return true
	}
	if !ok {
		reply(bot, chatID, "Слот уже заняли. Обнови список и выбери другой.")
		return true
	}

	// перечитываем слот (теперь booked_by_id заполнен)
	slot, _ = db.GetSlotByID(ctx, database, slotID)
	_ = SendConsultBookedNotification(ctx, bot, database, *slot, time.Local)

	when := slot.StartAt.In(time.Local).Format("02.01.2006 15:04")
	reply(bot, chatID, "Запись оформлена на "+when+". Напоминание придёт за 24ч и за 1ч.")
	return true
}

// --- util ---
func fields(s string) []string {
	return strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool { return unicode.IsSpace(r) })
}
