package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/observability"
)

// TryHandleTeacherMySlots /t_myslots — сначала дни текущей недели
func TryHandleTeacherMySlots(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" || !strings.HasPrefix(msg.Text, "/t_myslots") {
		return false
	}
	u, _ := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if u == nil || u.Role == nil || *u.Role != models.Teacher {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Доступно только учителям.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	loc := time.Local
	now := time.Now().In(loc)
	weekStart := now.AddDate(0, 0, -int((int(now.Weekday())+6)%7)) // понедельник
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		lbl := d.Format("Mon 02.01")
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(lbl, fmt.Sprintf("t_ms:day:%s", d.Format("2006-01-02"))),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_ms:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(msg.Chat.ID, "Выберите день недели:")
	out.ReplyMarkup = kb
	if _, err := tg.Send(bot, out); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return true
}

func TryHandleTeacherManageCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" {
		return false
	}

	switch {
	case strings.HasPrefix(cb.Data, "t_ms:cancel"):
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Отменено.")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true

	case strings.HasPrefix(cb.Data, "t_ms:back"):
		// перерисовать дни недели
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, "/t_myslots")
		msg.Text = "/t_myslots"
		TryHandleTeacherMySlots(ctx, bot, database, &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: cb.Message.Chat.ID}, Text: "/t_myslots"})
		return true

	case strings.HasPrefix(cb.Data, "t_ms:day:"):
		u, _ := db.GetUserByTelegramID(ctx, database, cb.Message.Chat.ID)
		if u == nil || u.Role == nil || *u.Role != models.Teacher {
			return true
		}

		day, _ := time.Parse("2006-01-02", strings.TrimPrefix(cb.Data, "t_ms:day:"))
		loc := time.Local
		from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		to := from.Add(24 * time.Hour)

		slots, err := db.ListTeacherSlotsRange(ctx, database, u.ID, from, to, 200)
		if err != nil {
			observability.CaptureErr(err)
			_ = cbErr(bot, cb, "Ошибка")
			return true
		}
		if len(slots) == 0 {
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "На выбранный день слотов нет.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, s := range slots {
			label := fmt.Sprintf("%s–%s (class:%d) #%d",
				s.StartAt.In(loc).Format("15:04"), s.EndAt.In(loc).Format("15:04"), s.ClassID, s.ID)
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
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", "t_ms:back"),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_ms:cancel"),
		))
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Слоты на выбранный день:")
		edit.ReplyMarkup = &kb
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}

	// ниже — уже были хендлеры t_del:/t_cancel: (оставляем)
	if strings.HasPrefix(cb.Data, "t_del:") || strings.HasPrefix(cb.Data, "t_cancel:") {
		// существующая логика удаления/отмены
	}
	return false
}

func cbErr(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, text string) error {
	_, err := bot.Request(tgbotapi.NewCallback(cb.ID, text))
	return err
}
