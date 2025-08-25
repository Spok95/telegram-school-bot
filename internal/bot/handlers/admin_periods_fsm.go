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
	"github.com/Spok95/telegram-school-bot/internal/models"
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
	perAdmOpen     = "peradm_open"
	perAdmCreate   = "peradm_create"
	perAdmEditPref = "peradm_edit_"

	editStepStartOnly = 1
	editStepAskStart  = 2
	editStepAskEnd    = 3
	editStepConfirm   = 4
)

// Старт: список периодов + «Создать / Изменить»
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
		if p.IsActive {
			tag = " — активный"
		} else if p.StartDate.After(now) {
			tag = " — будущий"
		} else if p.EndDate.Before(now) {
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
	sent, _ := bot.Send(msgOut)
	st.MessageID = sent.MessageID
}

// Колбэки списка
func HandleAdminPeriodsCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	st := periodsStates[chatID]
	if st == nil {
		return
	}
	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, ""))
	data := cb.Data

	if data == perAdmCancel {
		disable := tgbotapi.NewEditMessageReplyMarkup(chatID, st.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		bot.Request(disable)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Отменено."))
		delete(periodsStates, chatID)
		return
	}
	if data == perAdmBack {
		if st.Editing != nil {
			showEditCard(bot, chatID, st.Editing)
			return
		}
		disable := tgbotapi.NewEditMessageReplyMarkup(chatID, st.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		bot.Request(disable)
		bot.Send(tgbotapi.NewMessage(chatID, "↩️ Возврат в меню."))
		delete(periodsStates, chatID)
		return
	}
	if data == perAdmCreate {
		delete(periodsStates, chatID)
		StartSetPeriodFSM(bot, cb.Message) // переиспользуем создание
		return
	}
	if strings.HasPrefix(data, perAdmEditPref) {
		idStr := strings.TrimPrefix(data, perAdmEditPref)
		pid64, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)

		fmt.Println()
		fmt.Println("pid64, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)", pid64)
		fmt.Println()

		if err != nil || pid64 <= 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Некорректный идентификатор периода. Попробуйте обновить список."))
			return
		}
		p, err := db.GetPeriodByID(database, int(pid64))

		fmt.Println()
		fmt.Println("db.GetPeriodByID(database, int(pid64))", p)
		fmt.Println()

		if errors.Is(err, sql.ErrNoRows) || p == nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Период не найден в базе. Обновите список периодов."))
			return
		}
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Ошибка БД: %v", err)))
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
	bot.Send(edit)
}

// Текстовые шаги редактирования
func HandleAdminPeriodsText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
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
			bot.Send(m)
			return
		}
		ep.StartDate = d
		ep.Step = editStepAskEnd
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
		m := tgbotapi.NewMessage(chatID, "Введите дату окончания периода (ДД.ММ.ГГГГ):")
		m.ReplyMarkup = mk
		bot.Send(m)
	case editStepAskEnd:
		d, err := parseDate(msg.Text)
		if err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, "❌ Неверная дата. Введите окончание (ДД.ММ.ГГГГ):")
			m.ReplyMarkup = mk
			bot.Send(m)
			return
		}
		ep.EndDate = d
		if err := validateEditDates(ep); err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, err.Error())
			m.ReplyMarkup = mk
			bot.Send(m)
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
		bot.Send(m)
	}
}

// Колбэки редактора
func HandleAdminPeriodsEditCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	st := periodsStates[chatID]
	if st == nil || st.Editing == nil {
		return
	}
	ep := st.Editing
	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, ""))
	switch cb.Data {
	case "peradm_edit_end":
		ep.Step = editStepAskEnd
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
		m := tgbotapi.NewMessage(chatID, "Введите новую дату окончания (ДД.ММ.ГГГГ):")
		m.ReplyMarkup = mk
		bot.Send(m)
	case "peradm_edit_both":
		ep.Step = editStepAskStart
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
		m := tgbotapi.NewMessage(chatID, "Введите новую дату начала (ДД.ММ.ГГГГ):")
		m.ReplyMarkup = mk
		bot.Send(m)
	case "peradm_save":
		if err := validateEditDates(ep); err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow(perAdmBack, perAdmCancel))
			m := tgbotapi.NewMessage(chatID, err.Error())
			m.ReplyMarkup = mk
			bot.Send(m)
			return
		}
		if err := db.UpdatePeriod(database, models.Period{
			ID:        int64(ep.PeriodID),
			Name:      ep.Name,
			StartDate: ep.StartDate,
			EndDate:   ep.EndDate,
			IsActive:  false,
		}); err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось сохранить изменения."))
			return
		}
		_ = db.SetActivePeriod(database) // пересчитать активный
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Период обновлён."))
		if p, _ := db.GetPeriodByID(database, ep.PeriodID); p != nil {
			ep.StartDate, ep.EndDate, ep.IsActive = p.StartDate, p.EndDate, p.IsActive
		}
		showEditCard(bot, chatID, ep)
	}
}

func validateEditDates(ep *EditPeriodState) error {
	now := time.Now().Truncate(24 * time.Hour)
	if ep.EndDate.Before(now) && !ep.IsActive {
		return fmt.Errorf("❌ Нельзя изменять прошедшие периоды.")
	}
	if ep.IsActive && ep.EndDate.Before(now) {
		return fmt.Errorf("❌ Для активного периода дата окончания не может быть раньше сегодняшней.")
	}
	if ep.StartDate.After(ep.EndDate) {
		return fmt.Errorf("❌ Дата окончания не может быть раньше даты начала.")
	}
	return nil
}

// helper для dispatcher
func PeriodsFSMActive(chatID int64) (*PeriodsFSMState, bool) {
	st, ok := periodsStates[chatID]
	return st, ok
}
