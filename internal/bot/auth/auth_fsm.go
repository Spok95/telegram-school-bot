package auth

import (
	"context"
	"database/sql"

	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func StartRegistration(ctx context.Context, chatID int64, role string, bot *tgbotapi.BotAPI) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	switch role {
	case string(models.Student):
		StartStudentRegistration(ctx, chatID, bot)
	case string(models.Teacher), string(models.Administration):
		StartStaffRegistration(ctx, chatID, bot)
	}
}

func HandleFSMMessage(ctx context.Context, chatID int64, msg string, role string, bot *tgbotapi.BotAPI, database *sql.DB) {
	switch role {
	case string(models.Student):
		HandleStudentFSM(ctx, chatID, msg, bot, database)
	case string(models.Parent):
		HandleParentFSM(ctx, chatID, msg, bot, database)
	case string(models.Teacher), string(models.Administration):
		HandleStaffFSM(ctx, chatID, msg, bot, database, role)
	}
}
