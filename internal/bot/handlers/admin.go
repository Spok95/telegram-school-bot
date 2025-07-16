package handlers

import (
	"database/sql"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strconv"
	"strings"
)

type RoleRequest struct {
	ID              int64  `json:"id"`
	TelegramID      int64  `json:"telegram_id"`
	Name            string `json:"name"`
	PendingRole     string `json:"pending_role"`
	PendingFIO      string `json:"pending_fio"`
	PendingClass    string `json:"pending_class"`
	PendingChild    string `json:"pending_child"`
	PendingChildFIO string `json:"pending_childfio"`
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
SELECT id, telegram_id, name, pending_role, pending_fio, pending_class, pending_childfio
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
		if err := rows.Scan(&req.ID, &req.TelegramID, &req.Name, &req.PendingRole, &req.PendingFIO, &req.PendingClass, &req.PendingChildFIO); err != nil {
			continue
		}
		count++

		// Формируем сообщение и кнопки
		text := fmt.Sprintf("📋 Заявка от: %s\nTelegram ID: %d\nЖелаемая роль: %s\nКласс: %s\nРебёнок: %s",
			req.PendingFIO, req.TelegramID, req.PendingRole, req.PendingClass, req.PendingChildFIO)

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
func HandlePendingRoleCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	data := cb.Data

	if strings.HasPrefix(data, "approve_") {
		// approve_123456789_student
		parts := strings.Split(data, "_")
		if len(parts) != 3 {
			bot.Request(tgbotapi.NewCallback(cb.ID, "Неверный формат подтверждения"))
			return
		}

		userIDStr := parts[1]
		role := parts[2]

		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			bot.Request(tgbotapi.NewCallback(cb.ID, "Ошибка ID пользователя"))
			return
		}

		// 1. Прочитаем pending_* поля пользователя
		var pendingFio, pendingClass, pendingChildFio string
		err = database.QueryRow(`
SELECT pending_fio, pending_class, pending_childfio
FROM users WHERE telegram_id = ?`, userID).Scan(&pendingFio, &pendingClass, &pendingChildFio)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Не удалось прочитать заявку."))
			return
		}

		var name interface{}
		var childID *int64

		name = pendingFio

		if role == "parent" && pendingChildFio != "" {
			// Найти id ребенка по ФИО (и роли)
			var foundChildID int64
			err := database.QueryRow(
				`SELECT id FROM users WHERE name = ? AND role = 'student' LIMIT 1`,
				pendingChildFio).Scan(&foundChildID)
			if err != nil {
				childID = &foundChildID
			}
		}

		// 2. Перенести все значения, очистить pending_*
		_, err = database.Exec(`
			UPDATE users
			SET
				name = ?,
				role = ?,
				pending_role = NULL,
				pending_fio = NULL,
				pending_class = NULL,
				pending_childfio = NULL,
				class_name = ?,
				child_id = ?
			WHERE telegram_id = ?`,
			name, role, pendingClass, childID, userID)
		if err != nil {
			log.Println("Ошибка при обновлении роли:", err)
			bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Ошибка при подтверждении роли."))
			return
		}

		bot.Request(tgbotapi.NewCallback(cb.ID, "Роль подтверждена"))

		// Уведомляем пользователя
		msg := tgbotapi.NewMessage(userID, fmt.Sprintf("✅ Ваша роль *%s* подтверждена администратором!", role))
		msg.ParseMode = "Markdown"
		bot.Send(msg)

		AuthFSMDeleteSession(userID)

		// Повторно выводим меню для пользователя с новой ролью
		fakeMsg := &tgbotapi.Message{
			From: &tgbotapi.User{ID: userID},
			Chat: &tgbotapi.Chat{ID: userID},
		}
		HandleStart(bot, database, fakeMsg)
	} else if strings.HasPrefix(data, "reject_") {
		// reject_123456789
		parts := strings.Split(data, "_")
		if len(parts) != 2 {
			bot.Request(tgbotapi.NewCallback(cb.ID, "Неверный формат отклонения"))
			return
		}

		userIDStr := parts[1]
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			bot.Request(tgbotapi.NewCallback(cb.ID, "Ошибка ID пользователя"))
			return
		}

		// Удаляем все pending_* поля
		_, err = database.Exec(`UPDATE users SET pending_role = NULL, pending_fio = NULL, pending_class = NULL, pending_childfio = NULL WHERE telegram_id = ?`, userID)
		if err != nil {
			log.Println("Ошибка при отклонении заявки:", err)
			bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Ошибка при отклонении."))
			return
		}

		AuthFSMDeleteSession(userID)

		bot.Request(tgbotapi.NewCallback(cb.ID, "Заявка отклонена"))
		// Уведомляем пользователя
		bot.Send(tgbotapi.NewMessage(userID, "❌ Ваша заявка на роль была отклонена администратором."))
	}
}
