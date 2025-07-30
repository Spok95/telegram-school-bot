package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"os"
	"strconv"
)

var UserFSMRole = make(map[int64]string)

func EnsureAdmin(chatID int64, database *sql.DB, text string, bot *tgbotapi.BotAPI) {
	adminID, _ := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)

	if chatID == adminID && text == "/start" {
		SetUserFSMRole(chatID, "admin")
		_, err := database.Exec(`INSERT OR REPLACE INTO users (telegram_id, name, role, confirmed) VALUES (?, ?, ?, 1)`,
			chatID, "Администратор", models.Admin)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка авторизации админа."))
			return
		}
		_, err = database.Exec(`
            INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
            VALUES (
                (SELECT id FROM users WHERE telegram_id = ?),
                '', 'admin', ?, datetime('now')
            )
        `, adminID, adminID)
		if err != nil {
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Вы авторизованы как администратор"))
		return
	}
	return
}

func SetUserFSMRole(chatID int64, role string) {
	UserFSMRole[chatID] = role
}
