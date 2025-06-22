package db

import (
	"github.com/Spok95/telegram-school-bot/internal/models"
	"log"
)

func SetUserRole(telegramID int64, name string, role models.Role) error {
	query := `
INSERT INTO users (telegram_id, name, role)
VALUES (?, ?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET role=excluded.role, name=excluded.name;`

	_, err := DB.Exec(query, telegramID, name, string(role))
	if err != nil {
		log.Println("Error setting user's role:", err)
	}
	return err
}
