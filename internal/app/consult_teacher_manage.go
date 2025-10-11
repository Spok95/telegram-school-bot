package app

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/observability"
)

// TryHandleTeacherMySlots /t_myslots — список на 14 дней
func TryHandleTeacherMySlots(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" || !strings.HasPrefix(msg.Text, "/t_myslots") {
		return false
	}
	u, _ := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if u == nil || u.Role == nil || *u.Role != models.Teacher {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Доступно только учителям."))
		return true
	}
	from := time.Now()
	to := from.AddDate(0, 0, 14)
	slots, err := db.ListTeacherSlotsRange(ctx, database, u.ID, from, to, 200)
	if err != nil {
		observability.CaptureErr(err)
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при получении слотов."))
		return true
	}
	if len(slots) == 0 {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "В ближайшие 14 дней слотов нет."))
		return true
	}

	loc := time.Local
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range slots {
		label := fmt.Sprintf("%s–%s (class:%d) #%d",
			s.StartAt.In(loc).Format("02.01 15:04"),
			s.EndAt.In(loc).Format("15:04"),
			s.ClassID, s.ID,
		)
		if s.BookedByID.Valid {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Отменить "+label, fmt.Sprintf("t_cancel:%d", s.ID)),
			))
		} else {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Удалить "+label, fmt.Sprintf("t_del:%d", s.ID)),
			))
		}
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(msg.Chat.ID, "Мои слоты (14 дней):")
	out.ReplyMarkup = kb
	_, _ = bot.Send(out)
	return true
}

// TryHandleTeacherManageCallback колбэки t_del:<id> и t_cancel:<id>
func TryHandleTeacherManageCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" {
		return false
	}
	if !(strings.HasPrefix(cb.Data, "t_del:") || strings.HasPrefix(cb.Data, "t_cancel:")) {
		return false
	}
	u, _ := db.GetUserByTelegramID(ctx, database, cb.Message.Chat.ID)
	if u == nil || u.Role == nil || *u.Role != models.Teacher {
		return true
	}
	// парсим id
	sid := strings.TrimPrefix(strings.TrimPrefix(cb.Data, "t_del:"), "t_cancel:")
	slotID, err := strconv.ParseInt(sid, 10, 64)
	if err != nil || slotID <= 0 {
		_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Неверный слот"))
		return true
	}

	if strings.HasPrefix(cb.Data, "t_del:") {
		ok, err := db.DeleteFreeSlot(ctx, database, u.ID, slotID)
		if err != nil {
			observability.CaptureErr(err)
		}
		if !ok {
			_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Нельзя удалить: занят или не ваш"))
			return true
		}
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Слот удалён.")
		_, _ = bot.Send(edit)
		_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Удалено"))
		return true
	}

	// cancel
	parentID, ok, err := db.CancelBookedSlot(ctx, database, u.ID, slotID, "teacher_cancel")
	if err != nil {
		observability.CaptureErr(err)
	}
	if !ok {
		_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Нельзя отменить: свободен или не ваш"))
		return true
	}
	slot, _ := db.GetSlotByID(ctx, database, slotID)
	if parentID != nil && slot != nil {
		_ = SendTeacherCancelNotification(ctx, bot, database, *parentID, *slot, time.Local)
	}
	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Запись отменена.")
	_, _ = bot.Send(edit)
	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Отменено"))
	return true
}
