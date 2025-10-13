package app

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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

// StartParentConsultFlow entry из кнопки
func StartParentConsultFlow(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	u, err := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Parent {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Недоступно. Только для родителей.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	children, err := db.ListChildrenForParent(ctx, database, u.ID)
	if err != nil {
		observability.CaptureErr(err)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при получении списка детей.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if len(children) == 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "В системе не найден ни один ребёнок, привязанный к вашему профилю.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, ch := range children {
		title := ch.Name
		if ch.ClassNum.Valid && ch.ClassLet.Valid {
			title = fmt.Sprintf("%s (%d%s)", ch.Name, ch.ClassNum.Int64, strings.ToUpper(ch.ClassLet.String))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("p_pick_child:%d", ch.ID)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "p_flow:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(msg.Chat.ID, "Выберите ребёнка:")
	out.ReplyMarkup = kb
	if _, err := tg.Send(bot, out); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func TryHandleParentFlowCallbacks(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" {
		return false
	}

	switch {
	case strings.HasPrefix(cb.Data, "p_flow:cancel"):
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Отменено.")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true

	case strings.HasPrefix(cb.Data, "p_back:teachers"):
		// вернуться к выбору ребёнка
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, "/consult_help")
		msg.Text = "📅 Записаться на консультацию"
		StartParentConsultFlow(ctx, bot, database, &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: cb.Message.Chat.ID}})
		return true

	case strings.HasPrefix(cb.Data, "p_pick_child:"):
		childID, _ := strconv.ParseInt(strings.TrimPrefix(cb.Data, "p_pick_child:"), 10, 64)
		ch, err := db.GetUserByID(ctx, database, childID)
		if err != nil || ch.ID == 0 || (ch.ClassID == nil && (ch.ClassNumber == nil || ch.ClassLetter == nil)) {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "У ребёнка не указан класс")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		var teachers []db.TeacherLite
		loc := time.Local
		from := time.Now().In(loc).Truncate(24 * time.Hour)
		to := from.AddDate(0, 0, 7)
		if ch.ClassID != nil {
			// существующий DAO по class_id — но в явном диапазоне
			teachers, err = db.ListTeachersWithSlotsByClassRange(ctx, database, *ch.ClassID, from, to, 50)
		} else {
			teachers, err = db.ListTeachersWithSlotsByClassNLRange(ctx, database, *ch.ClassNumber, *ch.ClassLetter, from, to, 50)
		}
		if err != nil {
			observability.CaptureErr(err)
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ошибка при получении учителей")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		if len(teachers) == 0 {
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "В этом классе консультации не запланированы.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		var rows [][]tgbotapi.InlineKeyboardButton
		for _, t := range teachers {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.Name, fmt.Sprintf("p_pick_teacher:%d:%d", t.ID, childID)),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", "p_back:teachers"),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "p_flow:cancel"),
		))
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Выберите учителя:")
		edit.ReplyMarkup = &kb
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true

	case strings.HasPrefix(cb.Data, "p_pick_teacher:"):
		parts := strings.Split(cb.Data, ":")
		if len(parts) != 3 {
			return true
		}
		teacherID, _ := strconv.ParseInt(parts[1], 10, 64)
		childID, _ := strconv.ParseInt(parts[2], 10, 64)
		ch, err := db.GetUserByID(ctx, database, childID)
		if err != nil || ch.ID == 0 || ch.ClassID == nil {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Нет класса у ребёнка")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		loc := time.Local
		today := time.Now().In(loc).Truncate(24 * time.Hour)

		// 7 дат вперёд
		days := make([]time.Time, 0, 7)
		for i := 0; i < 7; i++ {
			days = append(days, today.AddDate(0, 0, i))
		}
		// порядок Пн→Вс
		sort.SliceStable(days, func(i, j int) bool {
			return weekdayKey(days[i].Weekday()) < weekdayKey(days[j].Weekday())
		})

		var rows [][]tgbotapi.InlineKeyboardButton
		for _, d := range days {
			lbl := fmt.Sprintf("%s %s", ruDayShort(d.Weekday()), d.Format("02.01"))
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					lbl,
					fmt.Sprintf("p_pick_date:%d:%d:%s", teacherID, childID, d.Format("2006-01-02")),
				),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", fmt.Sprintf("p_pick_child:%d", childID)),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "p_flow:cancel"),
		))
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Выберите дату:")
		edit.ReplyMarkup = &kb
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true

	case strings.HasPrefix(cb.Data, "p_pick_date:"):
		parts := strings.Split(cb.Data, ":")
		if len(parts) != 4 {
			return true
		}
		teacherID, _ := strconv.ParseInt(parts[1], 10, 64)
		childID, _ := strconv.ParseInt(parts[2], 10, 64)
		day, _ := time.Parse("2006-01-02", parts[3])

		loc := time.Local
		free, err := db.ListFreeSlotsByTeacherOnDate(ctx, database, teacherID, day, loc, 50)
		if err != nil {
			observability.CaptureErr(err)
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ошибка слотов")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		if len(free) == 0 {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Нет свободных на эту дату")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, s := range free {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("%s–%s", s.StartAt.In(loc).Format("15:04"), s.EndAt.In(loc).Format("15:04")),
					fmt.Sprintf("p_book:%d:%d", s.ID, childID),
				),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", fmt.Sprintf("p_pick_teacher:%d:%d", teacherID, childID)),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "p_flow:cancel"),
		))
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Свободные слоты:")
		edit.ReplyMarkup = &kb
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	return false
}
