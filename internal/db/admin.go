package db

import (
	"database/sql"
	"log"
	"os"
	"strconv"

	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var UserFSMRole = make(map[int64]string)

func EnsureAdmin(chatID int64, database *sql.DB, text string, bot *tgbotapi.BotAPI) {
	adminID, _ := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)

	if chatID == adminID && text == "/start" {
		SetUserFSMRole(chatID, "admin")

		// Проверяем, существует ли админ в базе
		var exists bool
		err := database.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = $1)`, chatID).Scan(&exists)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка авторизации админа."))
			return
		}

		if !exists {
			_, err := database.Exec(`INSERT INTO users (telegram_id, name, role, confirmed) VALUES ($1, $2, $3, TRUE)`,
				chatID, "Администратор", models.Admin)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка создания записи админа."))
				return
			}

			_, err = database.Exec(`
				INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
				VALUES (
					(SELECT id FROM users WHERE telegram_id = $1),
					'', 'admin', $2, NOW()
				)
			`, adminID, adminID)
			if err != nil {
				log.Println("❌ Ошибка записи в role_changes:", err)
			}
		}

		bot.Send(tgbotapi.NewMessage(chatID, "✅ Вы авторизованы как администратор"))
	}
}

func SetUserFSMRole(chatID int64, role string) {
	UserFSMRole[chatID] = role
}
