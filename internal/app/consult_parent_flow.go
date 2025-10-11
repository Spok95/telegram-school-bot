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

// StartParentConsultFlow Старт из кнопки "📅 Записаться на консультацию"
func StartParentConsultFlow(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	u, err := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Parent || u.ClassID == nil {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Недоступно. Проверьте роль и привязку к классу."))
		return
	}
	teachers, err := db.ListTeachersByClass(ctx, database, *u.ClassID, 50)
	if err != nil {
		observability.CaptureErr(err)
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при получении учителей."))
		return
	}
	if len(teachers) == 0 {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Для вашего класса учителя не найдены."))
		return
	}

	// строим клавиатуру
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, t := range teachers {
		label := fmt.Sprintf("%s (#%d)", t.Name, t.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("p_pick_teacher:%d", t.ID)),
		))
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(msg.Chat.ID, "Выберите учителя:")
	out.ReplyMarkup = kb
	_, _ = bot.Send(out)
}

// TryHandleParentPickTeacher Callback: p_pick_teacher:<teacherID> — показать даты 7 дней
func TryHandleParentPickTeacher(ctx context.Context, bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}
	if cb == nil || cb.Data == "" || !strings.HasPrefix(cb.Data, "p_pick_teacher:") {
		return false
	}
	teacherID, _ := strconv.ParseInt(strings.TrimPrefix(cb.Data, "p_pick_teacher:"), 10, 64)
	loc := time.Local

	var rows [][]tgbotapi.InlineKeyboardButton
	today := time.Now().In(loc)
	for i := 0; i < 7; i++ {
		d := today.AddDate(0, 0, i)
		lbl := d.Format("02.01 Mon")
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(lbl, fmt.Sprintf("p_pick_date:%d:%s", teacherID, d.Format("2006-01-02"))),
		))
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Выберите дату:")
	edit.ReplyMarkup = &kb
	_, _ = bot.Send(edit)
	return true
}

// TryHandleParentPickDate Callback: p_pick_date:<teacherID>:<YYYY-MM-DD> — показать свободные слоты кнопками
func TryHandleParentPickDate(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" || !strings.HasPrefix(cb.Data, "p_pick_date:") {
		return false
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 3 {
		return true
	}
	teacherID, _ := strconv.ParseInt(parts[1], 10, 64)
	day, _ := time.Parse("2006-01-02", parts[2])

	loc := time.Local
	free, err := db.ListFreeSlotsByTeacherOnDate(ctx, database, teacherID, day, loc, 30)
	if err != nil {
		observability.CaptureErr(err)
		_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Ошибка слотов"))
		return true
	}
	if len(free) == 0 {
		_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Нет свободных на эту дату"))
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
	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Свободные слоты:")
	edit.ReplyMarkup = &kb
	_, _ = bot.Send(edit)
	return true
}
