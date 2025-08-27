package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/Spok95/telegram-school-bot/internal/models"
)

func GetUserByTelegramID(database *sql.DB, telegramID int64) (*models.User, error) {
	query := `
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active, deactivated_at
FROM users WHERE telegram_id = $1`

	row := database.QueryRow(query, telegramID)

	var u models.User
	err := row.Scan(&u.ID, &u.TelegramID, &u.Name, &u.Role, &u.ClassID, &u.ClassName, &u.ClassNumber, &u.ClassLetter, &u.ChildID, &u.Confirmed, &u.IsActive, &u.DeactivatedAt)
	if err != nil {
		log.Println("Пользователь не найден в users", err)
		return nil, err
	}
	return &u, nil
}

func GetStudentsByClass(database *sql.DB, number int64, letter string) ([]models.User, error) {
	rows, err := database.Query(`
        SELECT id, name
        FROM users
        WHERE role = 'student'
          AND confirmed = TRUE
          AND is_active = TRUE
          AND class_number = $1 AND class_letter = $2
        ORDER BY name
    `, number, letter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var students []models.User
	for rows.Next() {
		var u models.User
		err := rows.Scan(&u.ID, &u.Name)
		if err != nil {
			return nil, err
		}
		students = append(students, u)
	}
	return students, nil
}

func GetChildrenByParentID(database *sql.DB, parentID int64) ([]models.User, error) {
	rows, err := database.Query(`
		SELECT u.id, u.name, u.class_number, u.class_letter
		FROM users u
		JOIN parents_students ps ON ps.student_id = u.id
		WHERE ps.parent_id = $1 AND u.is_active = TRUE
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
		SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active, deactivated_at
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
		&user.DeactivatedAt,
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
SELECT id, telegram_id, name, role, class_id, class_name, class_number, class_letter, child_id, confirmed, is_active, deactivated_at
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
			&u.ChildID, &u.Confirmed, &u.IsActive, &u.DeactivatedAt,
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

// ChangeRoleWithCleanup меняет роль и делает уборку (чистит класс/родительские связи) + аудит.
func ChangeRoleWithCleanup(database *sql.DB, targetUserID int64, newRole string, changedBy int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var oldRole string
	if err = tx.QueryRow(`SELECT role FROM users WHERE id=$1`, targetUserID).Scan(&oldRole); err != nil {
		return err
	}
	if newRole == "student" {
		return fmt.Errorf("use ChangeRoleToStudentWithAudit for student")
	}

	now := time.Now()

	// 1) Применяем новую роль и чистим школьные поля (для любого newRole ≠ student)
	if _, err = tx.Exec(`
		UPDATE users
		SET role=$1, class_id=NULL, class_name=NULL, class_number=NULL, class_letter=NULL
		WHERE id=$2
	`, newRole, targetUserID); err != nil {
		return err
	}

	// 2) Был STUDENT → стал НЕ-student: рвём связи «родитель–ребёнок» и заявки; пересчитываем активность родителей
	if oldRole == "student" && newRole != "student" {
		// Собираем всех родителей до удаления, чтобы потом пересчитать их активность
		parentIDs := []int64{}
		rows, err := tx.Query(`SELECT DISTINCT parent_id FROM parents_students WHERE student_id=$1`, targetUserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var pid int64
			if err := rows.Scan(&pid); err != nil {
				rows.Close()
				return err
			}
			parentIDs = append(parentIDs, pid)
		}
		rows.Close()

		if _, err = tx.Exec(`DELETE FROM parents_students WHERE student_id=$1`, targetUserID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM parent_link_requests WHERE student_id=$1`, targetUserID); err != nil {
			return err
		}
		// Пересчёт активности родителей внутри транзакции
		for _, pid := range parentIDs {
			var n int
			if err := tx.QueryRow(`
				SELECT COUNT(*)
				FROM parents_students ps
				JOIN users s ON s.id=ps.student_id
				WHERE ps.parent_id=$1 AND s.role='student' AND s.is_active=TRUE
			`, pid).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				if _, err := tx.Exec(`UPDATE users SET is_active=FALSE, deactivated_at=COALESCE(deactivated_at,$2) WHERE id=$1`, pid, now); err != nil {
					return err
				}
			} else {
				if _, err := tx.Exec(`UPDATE users SET is_active=TRUE WHERE id=$1`, pid); err != nil {
					return err
				}
			}
		}
	}

	// 3) Был PARENT → стал НЕ-parent: чистим связи/заявки и деактивируем
	if oldRole == "parent" && newRole != "parent" {
		if _, err = tx.Exec(`DELETE FROM parents_students WHERE parent_id=$1`, targetUserID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM parent_link_requests WHERE parent_id=$1`, targetUserID); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE users SET is_active=FALSE, deactivated_at=COALESCE(deactivated_at,$2) WHERE id=$1`,
			targetUserID, now); err != nil {
			return err
		}
	}

	// 4) Аудит
	if _, err = tx.Exec(`
		INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
		VALUES ($1,$2,$3,$4,$5)
	`, targetUserID, oldRole, newRole, changedBy, now); err != nil {
		return err
	}

	return tx.Commit()
}

// ChangeRoleToStudentWithAudit переводим в student + ставим класс и аудит.
func ChangeRoleToStudentWithAudit(database *sql.DB, targetUserID int64, classNumber int64, classLetter string, changedBy int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var oldRole string
	if err = tx.QueryRow(`SELECT role FROM users WHERE id = $1`, targetUserID).Scan(&oldRole); err != nil {
		return err
	}

	// Если был родителем — убрать родительские связи/заявки и деактивировать
	if oldRole == "parent" {
		if _, err = tx.Exec(`DELETE FROM parents_students WHERE parent_id=$1`, targetUserID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM parent_link_requests WHERE parent_id=$1`, targetUserID); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE users SET is_active=FALSE, deactivated_at=COALESCE(deactivated_at, NOW()) WHERE id=$1`, targetUserID); err != nil {
			return err
		}
	}

	// Назначаем роль student и класс
	if _, err = tx.Exec(`
		UPDATE users
		SET role='student', class_number=$1, class_letter=$2, class_name=NULL, class_id=NULL
		WHERE id=$3
	`, classNumber, classLetter, targetUserID); err != nil {
		return err
	}

	// Аудит
	now := time.Now()
	if _, err = tx.Exec(`
		INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
		VALUES ($1,$2,$3,$4,$5)
	`, targetUserID, oldRole, "student", changedBy, now); err != nil {
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

// DeactivateUser ставит is_active=false и фиксирует деактивацию
func DeactivateUser(database *sql.DB, userID int64, at time.Time) error {
	_, err := database.Exec(`UPDATE users SET is_active=FALSE, deactivated_at=COALESCE(deactivated_at, $2) WHERE id=$1`, userID, at)
	return err
}

// ActivateUser возвращает доступ (is_active=true), но deactivated_at не трогаем
func ActivateUser(database *sql.DB, userID int64) error {
	_, err := database.Exec(`UPDATE users SET is_active=TRUE WHERE id=$1`, userID)
	return err
}

// RefreshParentActiveFlag если нет активных детей — родитель становится неактивным
func RefreshParentActiveFlag(database *sql.DB, parentID int64) error {
	var n int
	err := database.QueryRow(`
		SELECT COUNT(*)
		FROM parents_students ps
		JOIN users s ON s.id = ps.student_id
		WHERE ps.parent_id = $1 AND s.role='student' AND s.is_active = TRUE
	`, parentID).Scan(&n)
	if err != nil {
		return err
	}

	if n == 0 {
		_, err = database.Exec(`UPDATE users SET is_active=FALSE, deactivated_at=COALESCE(deactivated_at, NOW()) WHERE id=$1`, parentID)
	} else {
		_, err = database.Exec(`UPDATE users SET is_active=TRUE WHERE id=$1`, parentID) // при наличии активных детей оживляем
	}
	return err
}

// GetAdminTelegramIDs — chat_id админов (admin + administration), только активные.
func GetAdminTelegramIDs(database *sql.DB) ([]int64, error) {
	rows, err := database.Query(`
		SELECT telegram_id
		FROM users
		WHERE role IN ('admin','administration')
		  AND is_active = TRUE
		  AND telegram_id IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
