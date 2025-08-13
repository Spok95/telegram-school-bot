package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"unicode"

	"github.com/Spok95/telegram-school-bot/internal/models"
)

func GetUserByTelegramID(db *sql.DB, telegramID int64) (*models.User, error) {
	query := `
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active
FROM users WHERE telegram_id = $1`

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
WHERE role = 'student' AND class_number = $1 AND class_letter = $2`

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
		WHERE ps.parent_id = $1
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
WHERE id = $1
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
	query := `SELECT id FROM classes WHERE number = $1 AND letter = $2`
	err := database.QueryRow(query, number, letter).Scan(&classID)
	if err != nil {
		log.Println("Ошибка при поиске class_id:", err)
		return 0, err
	}
	return classID, nil
}

// UpdateUserRoleWithAudit updates user's role and writes audit record to role_changes.
func UpdateUserRoleWithAudit(database *sql.DB, targetUserID int64, newRole string, changedBy int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var oldRole string
	if err = tx.QueryRow(`SELECT role FROM users WHERE id = $1`, targetUserID).Scan(&oldRole); err != nil {
		return err
	}

	if _, err = tx.Exec(`UPDATE users SET role = $1 WHERE id = $2`, newRole, targetUserID); err != nil {
		return err
	}

	if _, err = tx.Exec(`INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
	                     VALUES ($1, $2, $3, $4, NOW())`, targetUserID, oldRole, newRole, changedBy); err != nil {
		return err
	}

	return tx.Commit()
}

// FindUsersByQuery returns users matching name substring or class like "7А".
func FindUsersByQuery(database *sql.DB, q string, limit int) ([]models.User, error) {
	if limit <= 0 {
		limit = 50
	}

	// Варианты для ФИО
	qTrim := strings.TrimSpace(q)
	qTitle := ToTitleRU(qTrim)
	qUpper := toUpperRU(qTrim)

	// Вариант для класса
	qClass := normalizeClassQuery(qTrim) // как делали для 7a -> 7А

	// Ищем по имени в нескольких вариантах + по классу "7А"
	const query = `
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active
FROM users
WHERE name LIKE $1              -- TitleCase
   OR name LIKE $2              -- UPPER
   OR name LIKE $3              -- как ввели
   OR (CAST(class_number AS TEXT) || UPPER(class_letter)) LIKE $4
ORDER BY name ASC
LIMIT $5`

	rows, err := database.Query(
		query,
		"%"+qTitle+"%",
		"%"+qUpper+"%",
		"%"+qTrim+"%",
		"%"+qClass+"%",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []models.User
	for rows.Next() {
		var u models.User
		err = rows.Scan(
			&u.ID, &u.TelegramID, &u.Name, &u.Role,
			&u.ClassID, &u.ClassName, &u.ClassNumber, &u.ClassLetter,
			&u.ChildID, &u.Confirmed, &u.IsActive,
		)
		if err != nil {
			return nil, err
		}
		res = append(res, u)
	}
	return res, nil
}

func normalizeClassQuery(q string) string {
	pairs := map[rune]rune{
		'A': 'А', 'a': 'А',
		'B': 'В', 'b': 'В',
		'E': 'Е', 'e': 'Е',
		'K': 'К', 'k': 'К',
		'M': 'М', 'm': 'М',
		'H': 'Н', 'h': 'Н',
		'O': 'О', 'o': 'О',
		'P': 'Р', 'p': 'Р',
		'C': 'С', 'c': 'С',
		'T': 'Т', 't': 'Т',
		'X': 'Х', 'x': 'Х',
	}
	var out []rune
	for _, r := range strings.TrimSpace(q) {
		if rr, ok := pairs[r]; ok {
			r = rr
		}
		out = append(out, r)
	}
	return strings.ToUpper(string(out))
}

// ChangeRoleWithCleanup: меняет роль и делает уборку (чистит класс/родительские связи) + аудит.
func ChangeRoleWithCleanup(database *sql.DB, targetUserID int64, newRole string, changedBy int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var oldRole string
	if err = tx.QueryRow(`SELECT role FROM users WHERE id = $1`, targetUserID).Scan(&oldRole); err != nil {
		return err
	}

	// Если уходим со student — чистим класс
	if oldRole == "student" && newRole != "student" {
		if _, err = tx.Exec(`UPDATE users SET class_id=NULL, class_number=NULL, class_letter=NULL WHERE id=$1`, targetUserID); err != nil {
			return err
		}
	}
	// Если уходим с parent — рвём связи с детьми
	if oldRole == "parent" && newRole != "parent" {
		if _, err = tx.Exec(`DELETE FROM parents_students WHERE parent_id = $1`, targetUserID); err != nil {
			return err
		}
	}
	// Если новая роль не student — гарантированно нет класса
	if newRole != "student" {
		if _, err = tx.Exec(`UPDATE users SET role=$1, class_id=NULL, class_number=NULL, class_letter=NULL WHERE id=$2`, newRole, targetUserID); err != nil {
			return err
		}
	} else {
		// сюда не заходим — для student используем отдельную функцию (нужны номер/буква)
		return fmt.Errorf("use ChangeRoleToStudentWithAudit for student")
	}

	if _, err = tx.Exec(`INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
	                     VALUES ($1, $2, $3, $4, NOW())`, targetUserID, oldRole, newRole, changedBy); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeRoleToStudentWithAudit: переводим в student + ставим класс и аудит.
func ChangeRoleToStudentWithAudit(database *sql.DB, targetUserID int64, classNumber int64, classLetter string, changedBy int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var oldRole string
	if err = tx.QueryRow(`SELECT role FROM users WHERE id = $1`, targetUserID).Scan(&oldRole); err != nil {
		return err
	}

	// если был parent — порвём связи
	if oldRole == "parent" {
		if _, err = tx.Exec(`DELETE FROM parents_students WHERE parent_id = $1`, targetUserID); err != nil {
			return err
		}
	}

	// находим class_id
	cid, err := ClassIDByNumberAndLetter(database, classNumber, classLetter)
	if err != nil {
		return fmt.Errorf("класс %d%s не найден: %w", classNumber, classLetter, err)
	}

	if _, err = tx.Exec(`UPDATE users SET role='student', class_id=$1, class_number=$2, class_letter=$3 WHERE id=$4`,
		cid, classNumber, classLetter, targetUserID); err != nil {
		return err
	}

	if _, err = tx.Exec(`INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
	                     VALUES ($1, $2, 'student', $3, NOW())`, targetUserID, oldRole, changedBy); err != nil {
		return err
	}
	return tx.Commit()
}

func ToTitleRU(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) {
			prevSpace = true
			b.WriteRune(r)
			continue
		}
		if prevSpace {
			b.WriteRune(unicode.ToUpper(r))
			prevSpace = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Верхний регистр для кириллицы
func toUpperRU(s string) string {
	return strings.ToUpper(s) // в Go это работает и для кириллицы
}
