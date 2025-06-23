package db

import (
	"fmt"
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

func GetUserByTelegramID(telegramID int64) (*models.User, error) {
	row := DB.QueryRow(`SELECT id, telegram_id, name, role, class_id, child_id FROM users WHERE telegram_id = ?`, telegramID)

	log.Printf("[DB] Поиск пользователя с ID %d\n", telegramID)

	var u models.User

	fmt.Println("[DEBUG] Проверка наличия строки в SELECT...")

	err := row.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassID, &u.ChildID)
	if err != nil {

		log.Println("Ошибка при чтении пользователя:", err)

		return nil, err
	}
	return &u, nil
}

func GetAllStudents() ([]models.User, error) {
	query := `SELECT id, telegram_id, name, role, class_id, child_id FROM users WHERE role = 'student'`
	rows, err := DB.Query(query)
	if err != nil {
		log.Println("Ошибка при запросе учеников:", err)
		return nil, err
	}
	defer rows.Close()

	var students []models.User
	for rows.Next() {
		var u models.User
		err = rows.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassID, &u.ChildID)
		if err != nil {
			log.Println("Ошибка при чтении строки:", err)
			return nil, err
		}
		students = append(students, u)
	}
	return students, nil
}
