package handlers

import (
	"database/sql"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
)

type RoleRequest struct {
	ID          int64  `json:"id"`
	TelegramID  int64  `json:"telegram_id"`
	FullName    string `json:"full_name"`
	PendingRole string `json:"pending_role"`
}

// Обработка /pending_roles
func HandlePendingRoles(bot *tgbotapi.BotAPI, db *sql.DB, msg *tgbotapi.Message) {
	// Проверка: только админ
	var role string
	err := db.QueryRow(`SELECT role FROM users WHERE telegram_id = ?`, msg.From.ID).Scan(&role)
	if err != nil || role != "admin" {
		sendText(bot, msg.Chat.ID, "❌ Доступ запрещён. Только администратор может использовать эту команду.")
		return
	}

	// Получаем все заявки
	rows, err := db.Query(`
SELECT id, telegram_id, full_name, pending_role
FROM users
WHERE pending_role IS NOT NULL AND (role IS NULL OR role = '')
`)
	if err != nil {
		log.Println("Ошибка при выборке заявок:", err)
		sendText(bot, msg.Chat.ID, "❌ Ошибка при получении заявок.")
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var req RoleRequest
		if err := rows.Scan(&req.ID, &req.TelegramID, &req.FullName, &req.PendingRole); err != nil {
			continue
		}
		count++

		// Формируем сообщение и кнопки
		text := fmt.Sprintf("📋 Заявка от: %s\nTelegram ID: %d\nЖелаемая роль: %s", req.FullName, req.TelegramID, req.PendingRole)

		confirm := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_role:%d", req.TelegramID))
		reject := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_role:%d", req.TelegramID))

		keyboard := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(confirm, reject))

		msgOut := tgbotapi.NewMessage(msg.Chat.ID, text)
		msgOut.ReplyMarkup = keyboard
		bot.Send(msgOut)
	}

	if count == 0 {
		sendText(bot, msg.Chat.ID, "Нет активных заявок.")
	}
}

// Обработка нажатий на подтверждение/отклонение
func HandlePendingRoleCallback(bot *tgbotapi.BotAPI, db *sql.DB, cb *tgbotapi.CallbackQuery) {
	var action string
	var targetID int64

	_, err := fmt.Sscanf(cb.Data, "%[^:]:%d", &action, &targetID)
	if err != nil {
		_, err := bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Неверный формат команды"))
		if err != nil {
			log.Println(err)
		}
		return
	}

	var query string
	switch action {
	case "confirm_role":
		query = `UPDATE users SET role = pending_role, pending_role = NULL WHERE telegram_id = ?`
	case "reject_role":
		query = `UPDATE users SET pending_role = NULL WHERE telegram_id = ?`
	default:
		_, err := bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Неизвестное действие"))
		if err != nil {
			log.Println(err)
		}
		return
	}

	_, err = db.Exec(query, targetID)
	if err != nil {
		log.Println("Ошибка обновления роли:", err)
		_, err := bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Ошибка при обновлении"))
		if err != nil {
			log.Println(err)
		}
		return
	}

	_, err = bot.Request(tgbotapi.NewCallback(cb.ID, "✅ Обновлено"))
	if err != nil {
		log.Println(err)
	}
	bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "✅ Заявка обработана."))
}
