package handlers

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strings"
	"time"
)

type SetPeriodState struct {
	Step      int
	Name      string
	StartDate time.Time
	EndDate   time.Time
}

var periodStates = make(map[int64]*SetPeriodState)

func StartSetPeriodFSM(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	periodStates[chatID] = &SetPeriodState{Step: 1}
	bot.Send(tgbotapi.NewMessage(chatID, "Введите название нового периода (например: 1 триместр 2025):"))
}

func HandleSetPeriodInput(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state, ok := periodStates[chatID]
	if !ok {
		return
	}

	text := strings.TrimSpace(msg.Text)

	switch state.Step {
	case 1:
		state.Name = text
		state.Step = 2
		bot.Send(tgbotapi.NewMessage(chatID, "Введите дату начала периода в формате YYYY-MM-DD:"))
	case 2:
		start, err := time.Parse("2006-01-02", text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату начала в формате YYYY-MM-DD."))
			return
		}
		state.StartDate = start
		state.Step = 3
		bot.Send(tgbotapi.NewMessage(chatID, "Введите дату окончания периода в формате YYYY-MM-DD:"))
	case 3:
		end, err := time.Parse("2006-01-02", text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату окончания в формате YYYY-MM-DD."))
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
		err = db.CreatePeriod(database, period)
		if err != nil {
			log.Println("❌ Ошибка при создании периода:", err)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось сохранить период."))
			return
		}

		// Сбрасываем старые периоды и активируем этот
		err = db.SetActivePeriod(database, db.GetLastInsertID(database))
		if err != nil {
			log.Println("❌ Ошибка при установке активного периода:", err)
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Новый период успешно создан и активирован."))
		delete(periodStates, chatID)
	}
}

func GetSetPeriodState(chatID int64) *SetPeriodState {
	return periodStates[chatID]
}
