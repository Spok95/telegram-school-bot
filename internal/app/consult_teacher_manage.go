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

func ruDayShort(wd time.Weekday) string {
	switch wd {
	case time.Monday:
		return "Пн"
	case time.Tuesday:
		return "Вт"
	case time.Wednesday:
		return "Ср"
	case time.Thursday:
		return "Чт"
	case time.Friday:
		return "Пт"
	case time.Saturday:
		return "Сб"
	default:
		return "Вс"
	}
}

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
	today := time.Now().In(loc).Truncate(24 * time.Hour)

	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < 7; i++ {
		d := today.AddDate(0, 0, i)
		lbl := fmt.Sprintf("%s %s", ruDayShort(d.Weekday()), d.Format("02.01"))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(lbl, fmt.Sprintf("t_ms:day:%s", d.Format("2006-01-02"))),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_ms:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(msg.Chat.ID, "Выберите день (7 дней вперёд):")
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

	// 1) День → показать слоты за день (уже работает у тебя)
	if strings.HasPrefix(cb.Data, "t_ms:") {
		switch {
		case strings.HasPrefix(cb.Data, "t_ms:cancel"):
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Отменено.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true

		case strings.HasPrefix(cb.Data, "t_ms:back"):
			// перерисовать список дней (7 дней вперёд)
			TryHandleTeacherMySlots(ctx, bot, database, &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: cb.Message.Chat.ID}, Text: "/t_myslots"})
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true

		case strings.HasPrefix(cb.Data, "t_ms:day:"):
			u, _ := db.GetUserByTelegramID(ctx, database, cb.Message.Chat.ID)
			if u == nil || u.Role == nil || *u.Role != models.Teacher {
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return true
			}
			loc := time.Local
			day, _ := time.Parse("2006-01-02", strings.TrimPrefix(cb.Data, "t_ms:day:"))
			from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
			to := from.Add(24 * time.Hour)

			slots, err := db.ListTeacherSlotsRange(ctx, database, u.ID, from, to, 200)
			if err != nil {
				observability.CaptureErr(err)
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ошибка")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return true
			}
			if len(slots) == 0 {
				edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "На выбранный день слотов нет.")
				if _, err := tg.Send(bot, edit); err != nil {
					metrics.HandlerErrors.Inc()
				}
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return true
			}
			var rows [][]tgbotapi.InlineKeyboardButton
			for _, s := range slots {
				// человекочитаемая метка класса
				className := fmt.Sprintf("класс:%d", s.ClassID)
				if cls, _ := db.GetClassByID(ctx, database, s.ClassID); cls != nil {
					className = fmt.Sprintf("класс:%d%s", cls.Number, strings.ToUpper(cls.Letter))
				}
				// отметка занятости
				mark := "⬜"
				if s.BookedByID.Valid {
					mark = "✅"
				}

				// общий лейбл
				label := fmt.Sprintf("%s %s–%s (%s) #%d",
					mark,
					s.StartAt.In(loc).Format("15:04"),
					s.EndAt.In(loc).Format("15:04"),
					className, s.ID)

				// далее как и было: если занят — «Отменить», если свободен — «Удалить»
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
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
	}

	// 2) Собственно удаление / отмена
	if strings.HasPrefix(cb.Data, "t_del:") || strings.HasPrefix(cb.Data, "t_cancel:") {
		u, _ := db.GetUserByTelegramID(ctx, database, cb.Message.Chat.ID)
		if u == nil || u.Role == nil || *u.Role != models.Teacher {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		sid := strings.TrimPrefix(strings.TrimPrefix(cb.Data, "t_del:"), "t_cancel:")
		slotID, err := strconv.ParseInt(sid, 10, 64)
		if err != nil || slotID <= 0 {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Неверный слот")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		if strings.HasPrefix(cb.Data, "t_del:") {
			ok, err := db.DeleteFreeSlot(ctx, database, u.ID, slotID)
			if err != nil {
				observability.CaptureErr(err)
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ошибка")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return true
			}
			if !ok {
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Нельзя удалить: занят или не ваш")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return true
			}
			// перерисуем текст
			edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Слот удалён.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Удалено")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		// отмена занятого: уведомить родителя
		parentID, ok, err := db.CancelBookedSlot(ctx, database, u.ID, slotID, "teacher_cancel")
		if err != nil {
			observability.CaptureErr(err)
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ошибка")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		if !ok {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Нельзя отменить: свободен или не ваш")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}
		if parentID != nil {
			_ = SendConsultCancelCards(ctx, bot, database, *parentID, slotID, cb.Message.Chat.ID, time.Local)
		}
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Отменено.")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Отменено")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}

	return false
}
