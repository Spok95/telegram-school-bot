package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	StepInputName = iota
	StepInputStart
	StepInputEnd
	StepConfirm
	perCancel        = "per_cancel"
	perBackToMenu    = "per_back_menu"  // с экрана ввода названия — это выход (как Отмена)
	perBackToName    = "per_back_name"  // к вводу названия
	perBackToStart   = "per_back_start" // к вводу даты начала
	perConfirmCreate = "per_confirm"
)

type SetPeriodState struct {
	Step      int
	Name      string
	StartDate time.Time
	EndDate   time.Time
	MessageID int
}

var periodStates = make(map[int64]*SetPeriodState)

func StartSetPeriodFSM(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	delete(periodStates, chatID)

	state := &SetPeriodState{Step: StepInputName}
	periodStates[chatID] = state

	mk := tgbotapi.NewInlineKeyboardMarkup(
		fsmutil.BackCancelRow(perBackToMenu, perCancel), // Назад = выход, Отмена = выход
	)
	perReplace(bot, chatID, state, "Введите название нового периода (например: 1 триместр 2025):", mk)
}

func HandleSetPeriodInput(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state, ok := periodStates[chatID]
	if !ok {
		return
	}

	switch state.Step {
	case StepInputName:
		state.Name = msg.Text
		state.Step = StepInputStart
		mk := tgbotapi.NewInlineKeyboardMarkup(
			fsmutil.BackCancelRow(perBackToName, perCancel),
		)
		perReplace(bot, chatID, state, "Введите дату начала периода в формате ДД.ММ.ГГГГ:", mk)
	case StepInputStart:
		start, err := parseDate(msg.Text)
		if err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(
				fsmutil.BackCancelRow(perBackToName, perCancel),
			)
			perReplace(bot, chatID, state, "❌ Неверный формат. Введите дату начала в формате ДД.ММ.ГГГГ:", mk)
			return
		}
		state.StartDate = start
		state.Step = StepInputEnd
		mk := tgbotapi.NewInlineKeyboardMarkup(
			fsmutil.BackCancelRow(perBackToStart, perCancel),
		)
		perReplace(bot, chatID, state, "Введите дату окончания периода в формате ДД.ММ.ГГГГ:", mk)
	case StepInputEnd:
		end, err := parseDate(msg.Text)
		if err != nil || end.Before(state.StartDate) {
			msgTxt := "❌ Неверная дата окончания. Введите в формате ДД.ММ.ГГГГ:"
			if err == nil && end.Before(state.StartDate) {
				msgTxt = "❌ Дата окончания раньше даты начала. Введите корректную дату окончания:"
			}
			mk := tgbotapi.NewInlineKeyboardMarkup(
				fsmutil.BackCancelRow(perBackToStart, perCancel),
			)
			perReplace(bot, chatID, state, msgTxt, mk)
			return
		}
		state.EndDate = end
		state.Step = StepConfirm

		preview := fmt.Sprintf(
			"Создать период:\n• %s\n• %s — %s?",
			state.Name,
			state.StartDate.Format("02.01.2006"),
			state.EndDate.Format("02.01.2006"),
		)
		mk := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", perConfirmCreate),
			),
			fsmutil.BackCancelRow(perBackToStart, perCancel),
		)
		perReplace(bot, chatID, state, preview, mk)
	}
}

func HandleSetPeriodCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	state := periodStates[chatID]
	if state == nil {
		return
	}

	data := cb.Data
	perAnswer(bot, cb)
	// Выходы — первыми
	if data == perCancel || data == perBackToMenu {
		perClearMarkup(bot, chatID, state)
		perSend(bot, chatID, state, "🚫 Отменено.", tgbotapi.NewInlineKeyboardMarkup())
		delete(periodStates, chatID)
		return
	}

	switch data {
	case perBackToName:
		state.Step = StepInputName
		mk := tgbotapi.NewInlineKeyboardMarkup(
			fsmutil.BackCancelRow(perBackToMenu, perCancel),
		)
		perReplace(bot, chatID, state, "Введите название нового периода (например: 1 триместр 2025):", mk)
		return

	case perBackToStart:
		state.Step = StepInputStart
		mk := tgbotapi.NewInlineKeyboardMarkup(
			fsmutil.BackCancelRow(perBackToName, perCancel),
		)
		perReplace(bot, chatID, state, "Введите дату начала периода в формате ДД.ММ.ГГГГ:", mk)
		return

	case perConfirmCreate:
		// Сохранение в БД
		period := models.Period{
			Name:      state.Name,
			StartDate: state.StartDate,
			EndDate:   state.EndDate,
		}
		if _, err := db.CreatePeriod(ctx, database, period); err != nil {
			mk := tgbotapi.NewInlineKeyboardMarkup(
				fsmutil.BackCancelRow(perBackToStart, perCancel),
			)
			perReplace(bot, chatID, state, fmt.Sprintf("❌ Ошибка сохранения: %v", err), mk)
			return
		}
		perClearMarkup(bot, chatID, state)
		perSend(bot, chatID, state, "✅ Новый период успешно создан.", tgbotapi.NewInlineKeyboardMarkup())
		delete(periodStates, chatID)
		return
	}
}

func GetSetPeriodState(chatID int64) *SetPeriodState {
	return periodStates[chatID]
}

func parseDate(input string) (time.Time, error) {
	layout := "02.01.2006"
	date, err := time.Parse(layout, input)
	if err != nil {
		return time.Time{}, err
	}
	if date.Year() < 2025 {
		return time.Time{}, fmt.Errorf("❌ Неверная дата. Убедитесь, что месяц есть в году или день существует в этом месяце (например, февраль — 28 или 29 дней).%w", err)
	}
	return date, nil
}

// Отправить новое сообщение с клавиатурой и удалить старое, чтобы оно было ниже в чате
func perReplace(bot *tgbotapi.BotAPI, chatID int64, state *SetPeriodState, text string, mk tgbotapi.InlineKeyboardMarkup) {
	// удалить предыдущее бот-сообщение (если было)
	if state.MessageID != 0 {
		if _, err := tg.Request(bot, tgbotapi.NewDeleteMessage(chatID, state.MessageID)); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
	msg := tgbotapi.NewMessage(chatID, text)
	if len(mk.InlineKeyboard) > 0 {
		msg.ReplyMarkup = mk
	}
	sent, _ := tg.Send(bot, msg)
	state.MessageID = sent.MessageID
}

// Ответить на нажатие кнопки (убирает крутилку у пользователя)
func perAnswer(bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// Отправить новое сообщение без удаления предыдущего
func perSend(bot *tgbotapi.BotAPI, chatID int64, st *SetPeriodState, text string, mk tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	if len(mk.InlineKeyboard) > 0 {
		msg.ReplyMarkup = mk
	}
	sent, _ := tg.Send(bot, msg)
	st.MessageID = sent.MessageID
}

// Убрать inline-клавиатуру у текущего бот-сообщения (делает кнопки неактивными)
func perClearMarkup(bot *tgbotapi.BotAPI, chatID int64, st *SetPeriodState) {
	if st.MessageID == 0 {
		return
	}
	empty := tgbotapi.NewEditMessageReplyMarkup(chatID, st.MessageID,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
	if _, err := tg.Request(bot, empty); err != nil {
		metrics.HandlerErrors.Inc()
	}
}
