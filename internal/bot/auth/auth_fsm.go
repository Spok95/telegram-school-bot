package auth

import (
	"database/sql"

	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func StartRegistration(chatID int64, role string, bot *tgbotapi.BotAPI, database *sql.DB) {
	switch role {
	case string(models.Student):
		StartStudentRegistration(chatID, role, bot, database)
	case string(models.Teacher), string(models.Administration):
		StartStaffRegistration(chatID, bot)
	}
}

func HandleFSMMessage(chatID int64, msg string, role string, bot *tgbotapi.BotAPI, database *sql.DB) {
	switch role {
	case string(models.Student):
		HandleStudentFSM(chatID, msg, bot, database)
	case string(models.Parent):
		HandleParentFSM(chatID, msg, bot, database)
	case string(models.Teacher), string(models.Administration):
		HandleStaffFSM(chatID, msg, bot, database, role)
	}
}
