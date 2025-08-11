package auth

import (
	"database/sql"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func StartRegistration(chatID int64, role string, bot *tgbotapi.BotAPI, database *sql.DB) {
	switch role {
	case "student":
		StartStudentRegistration(chatID, role, bot, database)
	case "teacher", "administration":
		StartStaffRegistration(chatID, role, bot, database)
	}
}

func HandleFSMMessage(chatID int64, msg string, role string, bot *tgbotapi.BotAPI, database *sql.DB) {
	switch role {
	case "student":
		HandleStudentFSM(chatID, msg, bot, database)
	case "parent":
		HandleParentFSM(chatID, msg, bot, database)
	case "teacher", "administration":
		HandleStaffFSM(chatID, msg, bot, database, role)
	}
}
