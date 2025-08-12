package auth

import (
	"database/sql"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type StaffFSMState string

const (
	StateStaffName StaffFSMState = "staff_name"
	StateStaffWait StaffFSMState = "staff_wait"
)

var staffFSM = make(map[int64]StaffFSMState)
var staffData = make(map[int64]string)

func StartStaffRegistration(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	staffFSM[chatID] = StateStaffName
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ваше ФИО:"))
}

func HandleStaffFSM(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB, role string) {
	trimmed := strings.TrimSpace(msg)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(staffFSM, chatID)
		delete(staffData, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново."))
		return
	}

	state := staffFSM[chatID]

	switch state {
	case StateStaffName:
		staffData[chatID] = msg
		staffFSM[chatID] = StateStaffWait

		id, err := SaveStaffRequest(database, chatID, msg, role)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка при сохранении заявки. Попробуйте позже."))
			delete(staffFSM, chatID)
			delete(staffData, chatID)
			return
		}
		handlers.NotifyAdminsAboutNewUser(bot, database, id)
		bot.Send(tgbotapi.NewMessage(chatID, "Заявка на регистрацию отправлена администратору. Ожидайте подтверждения."))

		delete(staffFSM, chatID)
		delete(staffData, chatID)
	}
}

func SaveStaffRequest(database *sql.DB, telegramID int64, name, role string) (int64, error) {
	res, err := database.Exec(`
		INSERT INTO users (telegram_id, name, role, confirmed)
		VALUES (?, ?, ?, 0)
	`, telegramID, name, role)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}
