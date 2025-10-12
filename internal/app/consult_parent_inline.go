package app

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/observability"
)

// TryHandleParentSlotsCommand /p_slots <teacher_id> <YYYY-MM-DD>
func TryHandleParentSlotsCommand(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" {
		return false
	}
	parts := fields(msg.Text)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "/p_slots") {
		return false
	}

	u, err := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Parent {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Команда доступна только родителям.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	if len(parts) < 3 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Использование: /p_slots <teacher_id> <YYYY-MM-DD>")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	teacherID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || teacherID <= 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "teacher_id должен быть положительным числом.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	day, err := time.Parse("2006-01-02", parts[2])
	if err != nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Дата в формате YYYY-MM-DD.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}

	loc := time.Local
	free, err := db.ListFreeSlotsByTeacherOnDate(ctx, database, teacherID, day, loc, 30)
	if err != nil {
		observability.CaptureErr(err)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при получении слотов.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	if len(free) == 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "На выбранный день свободных слотов нет.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range free {
		label := fmt.Sprintf("%s–%s (#%d)", s.StartAt.In(loc).Format("15:04"), s.EndAt.In(loc).Format("15:04"), s.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("p_book:%d", s.ID)),
		))
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Свободные слоты:")
	msgOut.ReplyMarkup = kb
	if _, err := tg.Send(bot, msgOut); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return true
}

// TryHandleParentBookCallback обработка нажатия кнопки брони: p_book:<slotID>:<childID>
func TryHandleParentBookCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" || !strings.HasPrefix(cb.Data, "p_book:") {
		return false
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 3 {
		return true
	}
	slotID, _ := strconv.ParseInt(parts[1], 10, 64)
	childID, _ := strconv.ParseInt(parts[2], 10, 64)

	chatID := cb.Message.Chat.ID
	u, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Parent {
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Только для родителей")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}

	slot, err := db.GetSlotByID(ctx, database, slotID)
	if err != nil || slot == nil || slot.BookedByID.Valid {
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Слот недоступен")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	child, _ := db.GetUserByID(ctx, database, childID) // значение, не указатель
	if child.ID == 0 || child.ClassID == nil || *child.ClassID != slot.ClassID {
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Неверный класс")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}

	ok, err := db.TryBookSlot(ctx, database, slotID, u.ID)
	if err != nil {
		observability.CaptureErr(err)
		_ = sendCb(bot, cb, "Ошибка брони")
		return true
	}
	if !ok {
		_ = sendCb(bot, cb, "Уже занято")
		return true
	}

	slot, _ = db.GetSlotByID(ctx, database, slotID)

	// уведомления карточками (parent/child — значения)
	_ = SendConsultBookedCard(ctx, bot, database, *slot, *u, child, time.Local)

	edit := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Запись оформлена.")
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Готово")); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return true
}

func sendCb(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, text string) error {
	_, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, text))
	return err
}
