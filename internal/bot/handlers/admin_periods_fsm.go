package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type PeriodsFSMState struct {
	MessageID int
	Step      int
	Editing   *EditPeriodState
}

type EditPeriodState struct {
	PeriodID  int
	Name      string
	StartDate time.Time
	EndDate   time.Time
	IsActive  bool
	MessageID int
	Step      int
}

var periodsStates = map[int64]*PeriodsFSMState{}

const (
	perAdmCancel   = "peradm_cancel"
	perAdmBack     = "peradm_back"
	perAdmCreate   = "peradm_create"
	perAdmEditPref = "peradm_edit_"

	editStepAskStart = 1
	editStepAskEnd   = 2
	editStepConfirm  = 3
)

// StartAdminPeriods Старт: список периодов + «Создать / Изменить»
func StartAdminPeriods(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := &PeriodsFSMState{}
	periodsStates[chatID] = state
	showPeriodsList(bot, database, chatID, state)
}

func showPeriodsList(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, st *PeriodsFSMState) {
	per, _ := db.ListPeriods(database)
	text := "📅 Периоды:\n"
	now := time.Now()
	for _, p := range per {
		tag := ""
		switch {
		case p.IsActive:
			tag = " — активный"
		case p.StartDate.After(now):
			tag = " — будущий"
		case p.EndDate.Before(now):
			tag = " — прошедший"
		}

		text += fmt.Sprintf("• %s (%s–%s)%s\n", p.Name, p.StartDate.Format("02.01.2006"), p.EndDate.Format("02.01.2006"), tag)
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range per {
		cb := fmt.Sprintf("%s%d", perAdmEditPref, p.ID)
		label := fmt.Sprintf("✏️ Изменить: %s", p.Name)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, cb)))
	}
	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Создать период", perAdmCreate)),
		tgbotapi.NewInlineKeyboardRow(fsmutil.BackCancelRow(perAdmBack, perAdmCancel)...),
	)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msgOut := tgbotapi.NewMessage(chatID, text)
	msgOut.ReplyMarkup = mk
	sent, _ := tg.Send(bot, msgOut)
	st.MessageID = sent.MessageID
}

// HandleAdminPeriodsCallback коллбэки списка
func HandleAdminPeriodsCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	st := periodsStates[chatID]
	if st == nil {
		return
	}
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
		metrics.HandlerErrors.Inc()
	}
	data := cb.Data

	switch data {
	case perAdmCancel:
		disable := tgbotapi.NewEditMessageReplyMarkup(chatID, st.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		if _, err := tg.Request(bot, disable); err != nil {
			metrics.HandlerErrors.Inc()
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Отменено.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(periodsStates, chatID)
		return
	case perAdmBack:
		if st.Editing != nil {
			showEditCard(bot, chatID, st.Editing)
			return
		}
		disable := tgbotapi.NewEditMessageReplyMarkup(chatID, st.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		if _, err := tg.Request(bot, disable); err != nil {
			metrics.HandlerErrors.Inc()
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "↩️ Возврат в меню.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(periodsStates, chatID)
		return
	case perAdmCreate:
		delete(periodsStates, chatID)
		StartSetPeriodFSM(bot, cb.Message) // переиспользуем создание
		return
	case perAdmEditPref:
		idStr := strings.TrimPrefix(data, perAdmEditPref)
		pid64, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)

		fmt.Println()
		fmt.Println("pid64, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)", pid64)
		fmt.Println()

		if err != nil || pid64 <= 0 {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Некорректный идентификатор периода. Попробуйте обновить список.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		p, err := db.GetPeriodByID(database, int(pid64))

		fmt.Println()
		fmt.Println("db.GetPeriodByID(database, int(pid64))", p)
		fmt.Println()

		if errors.Is(err, sql.ErrNoRows) || p == nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Период не найден в базе. Обновите список периодов.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Ошибка БД: %v", err))); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		ep := &EditPeriodState{PeriodID: int(pid64), Name: p.Name, StartDate: p.StartDate, EndDate: p.EndDate, IsActive: p.IsActive}
		st.Editing = ep
		showEditCard(bot, chatID, ep)
		return
	}
}

func showEditCard(bot *tgbotapi.BotAPI, chatID int64, ep *EditPeriodState) {
	tag := "прошедший"
	now := time.Now()
	if ep.IsActive {
		tag = "активный"
	} else if ep.StartDate.After(now) {
		tag = "будущий"
	}
	txt := fmt.Sprintf(
		"✏️ Изменение периода: %s\n%s–%s (%s)\n\nПравила:\n• Активный: начало менять нельзя; конец — не раньше сегодня.\n• Будущий: можно менять обе даты.\n• Прошедший: изменять нельзя.",
		ep.Name, ep.StartDate.Format("02.01.2006"), ep.EndDate.Format("02.01.2006"), tag,
	)
	var rows [][]tgbotapi.InlineKeyboardButton
	if ep.IsActive {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Изменить конец", "peradm_edit_end")))
	} else if ep.EndDate.After(now) || ep.StartDate.After(now) {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Изменить даты", "peradm_edit_both")))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(fsmutil.BackCancelRow(perAdmBack, perAdmCancel)...))
	edit := tgbotapi.NewEditMessageText(chatID, periodsStates[chatID].MessageID, txt)
	edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// HandleAdminPeriodsText Текстовые шаги редактирования
func HandleAdminPeriodsText(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	st := periodsStates[chatID]
	if st == nil || st.Editing == nil {
		return
	}
	ep := st.Editing
	switch ep.Step {
	case editStepAskStart:
		d, err := parseDate(msg.Text)
		if err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, "❌ Неверная дата. Введите начало в формате ДД.ММ.ГГГГ:")
			m.ReplyMarkup = mk
			if _, err := tg.Send(bot, m); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		ep.StartDate = d
		ep.Step = editStepAskEnd
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
		m := tgbotapi.NewMessage(chatID, "Введите дату окончания периода (ДД.ММ.ГГГГ):")
		m.ReplyMarkup = mk
		if _, err := tg.Send(bot, m); err != nil {
			metrics.HandlerErrors.Inc()
		}
	case editStepAskEnd:
		d, err := parseDate(msg.Text)
		if err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, "❌ Неверная дата. Введите окончание (ДД.ММ.ГГГГ):")
			m.ReplyMarkup = mk
			if _, err := tg.Send(bot, m); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		ep.EndDate = d
		if err := validateEditDates(ep); err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, err.Error())
			m.ReplyMarkup = mk
			if _, err := tg.Send(bot, m); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		ep.Step = editStepConfirm
		txt := fmt.Sprintf("Подтвердите изменение дат:\n%s — %s", ep.StartDate.Format("02.01.2006"), ep.EndDate.Format("02.01.2006"))
		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("✅ Сохранить", "peradm_save")),
			fsmutil.BackCancelRow(perAdmBack, perAdmCancel),
		}
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		m := tgbotapi.NewMessage(chatID, txt)
		m.ReplyMarkup = mk
		if _, err := tg.Send(bot, m); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

// HandleAdminPeriodsEditCallback Колбэки редактора
func HandleAdminPeriodsEditCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	st := periodsStates[chatID]
	if st == nil || st.Editing == nil {
		return
	}
	ep := st.Editing
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
		metrics.HandlerErrors.Inc()
	}
	switch cb.Data {
	case "peradm_edit_end":
		ep.Step = editStepAskEnd
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
		m := tgbotapi.NewMessage(chatID, "Введите новую дату окончания (ДД.ММ.ГГГГ):")
		m.ReplyMarkup = mk
		if _, err := tg.Send(bot, m); err != nil {
			metrics.HandlerErrors.Inc()
		}
	case "peradm_edit_both":
		ep.Step = editStepAskStart
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
		m := tgbotapi.NewMessage(chatID, "Введите новую дату начала (ДД.ММ.ГГГГ):")
		m.ReplyMarkup = mk
		if _, err := tg.Send(bot, m); err != nil {
			metrics.HandlerErrors.Inc()
		}
	case "peradm_save":
		if err := validateEditDates(ep); err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, err.Error())
			m.ReplyMarkup = mk
			if _, err := tg.Send(bot, m); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if err := db.UpdatePeriod(database, models.Period{
			ID:        int64(ep.PeriodID),
			Name:      ep.Name,
			StartDate: ep.StartDate,
			EndDate:   ep.EndDate,
			IsActive:  false,
		}); err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось сохранить изменения.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		_ = db.SetActivePeriod(database) // пересчитать активный
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Период обновлён.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		if p, _ := db.GetPeriodByID(database, ep.PeriodID); p != nil {
			ep.StartDate, ep.EndDate, ep.IsActive = p.StartDate, p.EndDate, p.IsActive
		}
		showEditCard(bot, chatID, ep)
	}
}

func validateEditDates(ep *EditPeriodState) error {
	// Сравниваем ТОЛЬКО по дате (без времени), в локальной таймзоне.
	normalize := func(t time.Time) time.Time {
		loc := time.Local
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	today := normalize(time.Now())
	start := normalize(ep.StartDate)
	end := normalize(ep.EndDate)

	// Прошедший период — менять нельзя вовсе.
	if !ep.IsActive && end.Before(today) {
		return fmt.Errorf("❌ Нельзя изменять прошедшие периоды")
	}
	// Активный период: конец не раньше сегодняшней даты (можно = сегодня).
	if ep.IsActive && end.Before(today) {
		return fmt.Errorf("❌ Для активного периода дата окончания не может быть раньше сегодняшней")
	}
	// Базовая логика: конец не раньше начала.
	if start.After(end) {
		return fmt.Errorf("❌ Дата окончания не может быть раньше даты начала")
	}
	return nil
}

// PeriodsFSMActive helper для dispatcher
func PeriodsFSMActive(chatID int64) (*PeriodsFSMState, bool) {
	st, ok := periodsStates[chatID]
	return st, ok
}
