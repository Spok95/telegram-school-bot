package handlers

import (
	"database/sql"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleSetRoleRequest(bot *tgbotapi.BotAPI, db *sql.DB, msg *tgbotapi.Message) {
	// Inline-кнопки с выбором роли
	buttons := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Ученик", "role_student"),
			tgbotapi.NewInlineKeyboardButtonData("Родитель", "role_parent"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Учитель", "role_teacher"),
		),
	)

	msgOut := tgbotapi.NewMessage(msg.Chat.ID, "🧭 Выберите свою роль:")
	msgOut.ReplyMarkup = buttons
	bot.Send(msgOut)
}
