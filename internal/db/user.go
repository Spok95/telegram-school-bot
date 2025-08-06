package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"log"
)

func GetUserByTelegramID(db *sql.DB, telegramID int64) (*models.User, error) {
	query := `
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active
FROM users WHERE telegram_id = ?`

	row := db.QueryRow(query, telegramID)

	var u models.User
	err := row.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassID, &u.ClassName, &u.ClassNumber, &u.ClassLetter, &u.ChildID, &u.Confirmed, &u.IsActive)
	if err != nil {
		log.Println("Пользователь не найден в users", err)
		return nil, err
	}
	return &u, nil
}

func GetStudentsByClass(database *sql.DB, number int64, letter string) ([]models.User, error) {
	query := `
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active
FROM users
WHERE role = 'student' AND class_number = ? AND class_letter = ?`

	rows, err := database.Query(query, number, letter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var students []models.User
	for rows.Next() {
		var u models.User
		err := rows.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassID, &u.ClassName, &u.ClassNumber, &u.ClassLetter, &u.ChildID, &u.Confirmed, &u.IsActive)
		if err != nil {
			return nil, err
		}
		students = append(students, u)
	}
	return students, nil
}

func GetChildrenByParentID(db *sql.DB, parentID int64) ([]models.User, error) {
	rows, err := db.Query(`
		SELECT u.id, u.name, u.class_number, u.class_letter
		FROM users u
		JOIN parents_students ps ON ps.student_id = u.id
		WHERE ps.parent_id = ?
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var students []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Name, &u.ClassNumber, &u.ClassLetter); err != nil {
			continue
		}
		students = append(students, u)
	}
	return students, nil
}

func GetUserByID(database *sql.DB, id int64) (models.User, error) {
	var user models.User
	query := `
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active
FROM users
WHERE id = ?
`
	err := database.QueryRow(query, id).Scan(
		&user.ID,
		&user.TelegramID,
		&user.Name,
		&user.Role,
		&user.ClassID,
		&user.ClassName,
		&user.ClassNumber,
		&user.ClassLetter,
		&user.ChildID,
		&user.Confirmed,
		&user.IsActive,
	)
	return user, err
}

func ClassIDByNumberAndLetter(database *sql.DB, number int64, letter string) (int64, error) {
	var classID int64
	query := `SELECT id FROM classes WHERE number = ? AND letter = ?`
	err := database.QueryRow(query, number, letter).Scan(&classID)
	if err != nil {
		log.Println("Ошибка при поиске class_id:", err)
		return 0, err
	}
	return classID, nil
}
