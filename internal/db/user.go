package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"log"
)

func SetUserRole(db *sql.DB, telegramID int64, name string, role models.Role) error {
	query := `
INSERT INTO users (telegram_id, name, role)
VALUES (?, ?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET role=excluded.role, name=excluded.name;`

	_, err := db.Exec(query, telegramID, name, string(role))
	if err != nil {
		log.Println("Error setting user's role:", err)
		return err
	}
	return err
}

func GetUserByTelegramID(db *sql.DB, telegramID int64) (*models.User, error) {
	query := `
SELECT id, telegram_id, name, role, class_name, child_id, pending_role, is_active
FROM users WHERE telegram_id = ?`

	row := db.QueryRow(query, telegramID)

	var u models.User
	err := row.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassName, &u.ChildID, &u.PendingRole, &u.IsActive)
	if err != nil {
		log.Println("Пользователь не найден в users", err)
		return nil, err
	}
	return &u, nil
}

func GetAllStudents(db *sql.DB) ([]models.User, error) {
	query := `
SELECT id, telegram_id, name, role, class_name, child_id, pending_role, is_active
FROM users WHERE role = 'student'`

	rows, err := db.Query(query)
	if err != nil {
		log.Println("Ошибка при запросе учеников:", err)
		return nil, err
	}
	defer rows.Close()

	var students []models.User
	for rows.Next() {
		var u models.User
		err = rows.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassName, &u.ChildID, &u.PendingRole, &u.IsActive)
		if err != nil {
			log.Println("Ошибка при чтении строки:", err)
			return nil, err
		}
		students = append(students, u)
	}
	return students, nil
}

func UpdateUserPendingApplication(database *sql.DB, telegramID int64, pendingRole, pendingFIO, pendingClass, pendingChildFIO string) error {
	query := `
UPDATE users SET
                 pending_role=?,
                 pending_fio=?,
                 pending_class=?,
                 pending_childfio=?
WHERE telegram_id=?`
	_, err := database.Exec(query, pendingRole, pendingFIO, pendingClass, pendingChildFIO, telegramID)
	if err != nil {
		log.Println("Ошибка при сохранении pending application:", err)
		return err
	}
	return err
}
