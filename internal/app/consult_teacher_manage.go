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
	// 14 дат вперёд — БЕЗ доп. сортировки (хронологический порядок)
	days := make([]time.Time, 0, 14)
	for i := 0; i < 14; i++ {
		days = append(days, today.AddDate(0, 0, i))
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, d := range days {
		// подпись в виде: 03.11 (Пн)
		lbl := fmt.Sprintf("%s (%s)", d.Format("02.01"), ruDayShort(d.Weekday()))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(lbl, fmt.Sprintf("t_ms:day:%s", d.Format("2006-01-02"))),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_ms:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(msg.Chat.ID, "Выберите день (14 дней вперёд):")
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
			// перерисовать список дней (14 дней вперёд)
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
			renderTeacherDaySlots(ctx, bot, database, cb.Message.Chat.ID, cb.Message.MessageID, u.ID, day, loc)
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
			// читаем ДО удаления, чтобы знать дату
			slotBefore, _ := db.GetSlotByID(ctx, database, slotID)

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
			// перерисовываем список на ту же дату
			if slotBefore != nil {
				day := slotBefore.StartAt.In(time.Local)
				day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
				renderTeacherDaySlots(ctx, bot, database, cb.Message.Chat.ID, cb.Message.MessageID, u.ID, day, time.Local)
			} else {
				// fallback
				edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Слот удалён.")
				if _, err := tg.Send(bot, edit); err != nil {
					metrics.HandlerErrors.Inc()
				}
			}
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Удалено")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		// --- t_cancel: сначала читаем слот, чтобы запомнить parentID и тайм-окно
		slotBefore, _ := db.GetSlotByID(ctx, database, slotID)
		var parentID int64
		if slotBefore != nil && slotBefore.BookedByID.Valid {
			parentID = slotBefore.BookedByID.Int64
		} else {
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Слот уже свободен")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return true
		}

		// Отменяем
		_, ok, err := db.CancelBookedSlot(ctx, database, u.ID, slotID, "teacher_cancel")
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

		// Карточки + бродкаст используем ДАННЫЕ ДО отмены
		_ = SendConsultCancelCards(ctx, bot, database, parentID, *slotBefore, time.Local)

		day := slotBefore.StartAt.In(time.Local)
		day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
		renderTeacherDaySlots(ctx, bot, database, cb.Message.Chat.ID, cb.Message.MessageID, u.ID, day, time.Local)
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Отменено")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	return false
}

// рендер списка слотов учителя на конкретную дату (как в t_ms:day)
func renderTeacherDaySlots(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, msgID int, teacherID int64, day time.Time, loc *time.Location) {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	to := from.Add(24 * time.Hour)

	slots, err := db.ListTeacherSlotsRange(ctx, database, teacherID, from, to, 200)
	if err != nil {
		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "Ошибка получения слотов"))
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, s := range slots {
		// соберём ВСЕ классы: первичный + связи consult_slot_classes
		classLabels := []string{}
		if cls, _ := db.GetClassByID(ctx, database, s.ClassID); cls != nil {
			classLabels = append(classLabels, fmt.Sprintf("%d%s", cls.Number, strings.ToUpper(cls.Letter)))
		}
		rowsCls, _ := database.QueryContext(ctx, `
            SELECT c.number, c.letter
            FROM consult_slot_classes csc
            JOIN classes c ON c.id = csc.class_id
            WHERE csc.slot_id = $1
            ORDER BY c.number, c.letter
        `, s.ID)
		if rowsCls != nil {
			for rowsCls.Next() {
				var num int
				var let string
				_ = rowsCls.Scan(&num, &let)
				lbl := fmt.Sprintf("%d%s", num, strings.ToUpper(let))
				dup := false
				for _, have := range classLabels {
					if have == lbl {
						dup = true
						break
					}
				}
				if !dup {
					classLabels = append(classLabels, lbl)
				}
			}
			_ = rowsCls.Close()
		}

		mark := "⬜"
		if s.BookedByID.Valid {
			mark = "✅"
		}
		label := fmt.Sprintf("%s %s–%s (класс: %s) #%d",
			mark,
			s.StartAt.Format("15:04"),
			s.EndAt.Format("15:04"),
			strings.Join(classLabels, ", "),
			s.ID)

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

	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", "t_ms:back"),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_ms:cancel"),
		),
	)
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	title := "Слоты на " + day.In(loc).Format("02.01.2006 (Mon)")
	edit := tgbotapi.NewEditMessageText(chatID, msgID, title)
	edit.ReplyMarkup = &kb
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}
