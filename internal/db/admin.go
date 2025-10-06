package db

import (
	"database/sql"
	"log"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var UserFSMRole = make(map[int64]string)

func EnsureAdmin(chatID int64, database *sql.DB, text string, bot *tgbotapi.BotAPI) {
	if IsAdminID(chatID) && text == "/start" {
		SetUserFSMRole(chatID, "admin")

		// Проверяем, существует ли админ в базе
		var exists bool
		err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = $1)`, chatID).Scan(&exists)
		if err != nil {
			if _, err := bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка авторизации админа.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		if !exists {
			_, err := database.Exec(`INSERT INTO users (telegram_id, name, role, confirmed, is_active) VALUES ($1, $2, $3, TRUE, TRUE) ON CONFLICT DO NOTHING`,
				chatID, "Админ", models.Admin)
			if err != nil {
				if _, err := bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка авторизации админа.")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}

			user, err := GetUserByTelegramID(database, chatID)
			if err != nil || user == nil {
				log.Println("❌ Не удалось получить admin user:", err)
			} else {
				if _, err = database.Exec(`
        			INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
        			VALUES (
            			(SELECT id FROM users WHERE telegram_id = $1),
            			'', 'admin', $2, NOW()
        			) ON CONFLICT DO NOTHING
				`, chatID, user.ID); err != nil {
					log.Println("❌ Ошибка записи в role_changes:", err)
				}
			}
		}

		if _, err := bot.Send(tgbotapi.NewMessage(chatID, "✅ Вы авторизованы как администратор")); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

func SetUserFSMRole(chatID int64, role string) {
	UserFSMRole[chatID] = role
}
