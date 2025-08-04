package handlers

import (
	"database/sql"
	"fmt"
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

func StartSetPeriodFSM(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
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
		bot.Send(tgbotapi.NewMessage(chatID, "Введите дату начала периода в формате ДД.ММ.ГГГГ:"))
	case StepInputStart:
		start, err := parseDate(msg.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату начала в формате ДД.ММ.ГГГГ."))
			return
		}
		state.StartDate = start
		state.Step = StepInputEnd
		bot.Send(tgbotapi.NewMessage(chatID, "Введите дату окончания периода в формате ДД.ММ.ГГГГ:"))
	case StepInputEnd:
		end, err := parseDate(msg.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату окончания в формате ДД.ММ.ГГГГ."))
			return
		}
		state.EndDate = end

		// Сохраняем период
		period := models.Period{
			Name:      state.Name,
			StartDate: state.StartDate,
			EndDate:   state.EndDate,
		}

		// Проверка корректности дат
		if !period.StartDate.Before(period.EndDate) {
			msg := tgbotapi.NewMessage(chatID, "❌ Ошибка: дата начала должна быть раньше даты окончания.\nПопробуйте снова.")
			bot.Send(msg)
			delete(periodStates, chatID) // сбрасываем FSM, если нужно
			return
		}
		// Автоматическая активация, если период включает сегодняшнюю дату
		now := time.Now()
		if !now.Before(period.StartDate) && !now.After(period.EndDate) {
			period.IsActive = true
		}

		_, err = db.CreatePeriod(database, period)
		if err != nil {
			log.Println("❌ Ошибка при создании периода:", err)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось сохранить период."))
			return
		}

		bot.Send(tgbotapi.NewMessage(chatID, "✅ Новый период успешно создан."))
		delete(periodStates, chatID)
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
		return time.Time{}, fmt.Errorf("❌ Неверная дата. Убедитесь, что месяц есть в году или день существует в этом месяце (например, февраль — 28 или 29 дней).")
	}
	return date, nil
}
