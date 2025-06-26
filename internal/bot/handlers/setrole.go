package handlers

import (
	"database/sql"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
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

func HandleRoleCallback(bot *tgbotapi.BotAPI, db *sql.DB, cb *tgbotapi.CallbackQuery) {
	telegramID := cb.From.ID
	var role string

	switch cb.Data {
	case "role_student":
		role = "student"
	case "role_teacher":
		role = "teacher"
	case "role_parent":
		role = "parent"
	default:
		_, err := bot.Request(tgbotapi.NewCallback(cb.ID, "Ошибка выбора"))
		if err != nil {
			log.Println(err)
		}
		return
	}

	_, err := db.Exec(`UPDATE users SET pending_role = ? WHERE telegram_id = ?`, role, telegramID)
	if err != nil {
		log.Println("Ошибка сохранения pending_role:", err)
		bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Не удалось сохранить выбор."))
		return
	}

	_, err = bot.Request(tgbotapi.NewCallback(cb.ID, "Заявка отправлена"))
	if err != nil {
		log.Println(err)
	}
	bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("✅ Ваша заявка на роль *%s* отправлена администратору.", role)))
}
