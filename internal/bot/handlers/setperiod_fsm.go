package handlers

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"time"
)

const (
	StepInputName = iota
	StepInputStart
	StepInputEnd
)

type SetPeriodState struct {
	Step      int
	Name      string
	StartDate time.Time
	EndDate   time.Time
}

var periodStates = make(map[int64]*SetPeriodState)

func StartSetPeriodFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	// 🔁 Сброс состояния перед запуском FSM
	delete(periodStates, chatID)

	// Запуск нового FSM
	periodStates[chatID] = &SetPeriodState{Step: StepInputName}
	bot.Send(tgbotapi.NewMessage(chatID, "Введите название нового периода (например: 1 триместр 2025):"))
}

func HandleSetPeriodInput(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state, ok := periodStates[chatID]
	if !ok {
		return
	}

	switch state.Step {
	case StepInputName:
		state.Name = msg.Text
		state.Step = StepInputStart
		bot.Send(tgbotapi.NewMessage(chatID, "Введите дату начала периода в формате YYYY-MM-DD:"))
	case StepInputStart:
		start, err := time.Parse("2006-01-02", msg.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату начала в формате YYYY-MM-DD."))
			delete(periodStates, chatID)
			return
		}
		state.StartDate = start
		state.Step = StepInputEnd
		bot.Send(tgbotapi.NewMessage(chatID, "Введите дату окончания периода в формате YYYY-MM-DD:"))
	case StepInputEnd:
		end, err := time.Parse("2006-01-02", msg.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату окончания в формате YYYY-MM-DD."))
			delete(periodStates, chatID)
			return
		}
		state.EndDate = end

		// Сохраняем период
		period := models.Period{
			Name:      state.Name,
			StartDate: state.StartDate,
			EndDate:   state.EndDate,
			IsActive:  true,
		}
		periodID, err := db.CreatePeriod(database, period)
		if err != nil {
			log.Println("❌ Ошибка при создании периода:", err)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось сохранить период."))
			delete(periodStates, chatID)
			return
		}

		// Сбрасываем старые периоды и активируем этот
		err = db.SetActivePeriod(database, periodID)
		if err != nil {
			log.Println("❌ Ошибка при установке активного периода:", err)
			delete(periodStates, chatID)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Новый период успешно создан и активирован."))
		delete(periodStates, chatID)
	}
}

func GetSetPeriodState(chatID int64) *SetPeriodState {
	return periodStates[chatID]
}
