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

//func HandleRoleCallback(bot *tgbotapi.BotAPI, db *sql.DB, cb *tgbotapi.CallbackQuery) {
//	telegramID := cb.From.ID
//	var role string
//
//	switch cb.Data {
//	case "role_student":
//		role = "student"
//	case "role_teacher":
//		role = "teacher"
//	case "role_parent":
//		role = "parent"
//	default:
//		return
//	}
//
//	// Обновляем pending_role
//	_, err := db.Exec(`UPDATE users SET pending_role = ? WHERE telegram_id = ?`, role, telegramID)
//	if err != nil {
//		log.Println("Ошибка сохранения pending_role:", err)
//		bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Не удалось сохранить выбор."))
//		return
//	}
//
//	// Уведомление пользователю
//	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, "Заявка отправлена"))
//	bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("✅ Ваша заявка на роль *%s* отправлена администратору.", role)))
//
//	// Получаем имя пользователя
//	name := cb.From.FirstName
//
//	// Находим админов
//	rows, err := db.Query(`SELECT telegram_id FROM users WHERE role = 'admin' AND is_active = 1`)
//	if err != nil {
//		log.Println("Ошибка при поиске администраторов:", err)
//		return
//	}
//	defer rows.Close()
//
//	for rows.Next() {
//		var adminID int64
//		if err := rows.Scan(&adminID); err != nil {
//			log.Println("Ошибка при чтении adminID:", err)
//			continue
//		}
//
//		text := fmt.Sprintf("🔔 Пользователь *%s* запросил роль *%s*", name, role)
//		msg := tgbotapi.NewMessage(adminID, text)
//		msg.ParseMode = "Markdown"
//
//		// Кнопки подтверждения/отклонения
//		buttons := tgbotapi.NewInlineKeyboardMarkup(
//			tgbotapi.NewInlineKeyboardRow(
//				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("approve_%d_%s", telegramID, role)),
//				tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", telegramID)),
//			),
//		)
//		msg.ReplyMarkup = buttons
//		bot.Send(msg)
//	}
//}
