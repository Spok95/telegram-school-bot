package db

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"log"
	"time"
)

func AddScore(database *sql.DB, score models.Score) error {
	query := `
INSERT INTO scores (
                    student_id, category_id, points, type, comment, status, approved_by, approved_at, created_by, created_at, period_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`

	if score.Type == "remove" {
		score.Points = -score.Points
	}

	_, err := database.Exec(query,
		score.StudentID,
		score.CategoryID,
		score.Points,
		score.Type,
		score.Comment,
		score.Status,
		score.ApprovedBy,
		score.ApprovedAt,
		score.CreatedBy,
		score.CreatedAt,
		score.PeriodID,
	)
	if err != nil {
		log.Println("Ошибка при добавлении записи о баллах:", err)
	}
	return err
}

func GetScoreByStudent(database *sql.DB, studentID int64) ([]models.Score, error) {
	rows, err := database.Query(`
SELECT s.id, s.student_id, s.category_id, c.name AS category, s.points, s.type, s.comment, s.status, s.approved_by, s.approved_at, s.created_by, s.created_at
FROM scores s 
JOIN categories c ON s.category_id = c.id
WHERE s.student_id = ? AND s.status = 'approved'
ORDER BY s.created_at DESC`, studentID)

	if err != nil {
		log.Println("Ошибка при получении истории баллов:", err)
		return nil, err
	}
	defer rows.Close()

	var scores []models.Score
	for rows.Next() {
		var s models.Score
		err := rows.Scan(&s.ID, &s.StudentID, &s.CategoryID, &s.CategoryLabel, &s.Points, &s.Type, &s.Comment, &s.Status, &s.ApprovedBy, &s.ApprovedAt, &s.CreatedBy, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		scores = append(scores, s)
	}
	return scores, nil
}

// GetPendingScores возвращает все заявки, ожидающие подтверждения
func GetPendingScores(database *sql.DB) ([]models.Score, error) {
	rows, err := database.Query(`
		SELECT s.id, s.student_id, s.category_id, c.name AS category_label, s.points, s.type, s.comment, s.created_by
		FROM scores s
		JOIN categories c ON c.id = s.category_id
		WHERE s.status = 'pending'
		ORDER BY s.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.Score
	for rows.Next() {
		var s models.Score
		err := rows.Scan(&s.ID, &s.StudentID, &s.CategoryID, &s.CategoryLabel, &s.Points, &s.Type, &s.Comment, &s.CreatedBy)
		if err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, nil
}

// ApproveScore подтверждает заявку и обновляет рейтинг ученика и класса
func ApproveScore(database *sql.DB, scoreID int64, adminID int64, approvedAt time.Time) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var studentID int64
	var points int
	var scoreType string
	var categoryID int64
	err = tx.QueryRow(`SELECT student_id, points, type, category_id FROM scores WHERE id = ? AND status = 'pending'`, scoreID).Scan(&studentID, &points, &scoreType, &categoryID)
	if err != nil {
		return fmt.Errorf("заявка не найдена: %v", err)
	}

	// Получаем активный период
	activePeriod, err := GetActivePeriod(database)
	var periodID *int64
	if err == nil && activePeriod != nil {
		periodID = &activePeriod.ID
	}

	// Обновляем заявку: статус + period_id
	_, err = tx.Exec(`
		UPDATE scores 
		SET status = 'approved', approved_by = ?, approved_at = ?, period_id = ? 
		WHERE id = ?`,
		adminID, approvedAt, periodID, scoreID,
	)
	if err != nil {
		return err
	}

	// Приводим к положительному числу (points уже может быть отрицательным)
	adjust := points
	if adjust < 0 {
		adjust = -adjust
	}

	// Обновляем коллективный рейтинг
	if scoreType == "add" {
		_, err = tx.Exec(`UPDATE classes SET collective_score = collective_score + ? WHERE id = (SELECT class_id FROM users WHERE id = ?)`, adjust*30/100, studentID)
		if err != nil {
			return err
		}
	} else if scoreType == "remove" && categoryID != 999 {
		_, err = tx.Exec(`UPDATE classes SET collective_score = collective_score - ? WHERE id = (SELECT class_id FROM users WHERE id = ?)`, adjust*30/100, studentID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func RejectScore(database *sql.DB, scoreID int64, adminID int64, rejectedAt time.Time) error {
	_, err := database.Exec(`UPDATE scores SET status = 'rejected', approved_by = ?, approved_at = ? WHERE id = ?`, adminID, rejectedAt, scoreID)
	return err
}

// GetScoreStatusByID возвращает статус заявки по ID
func GetScoreStatusByID(database *sql.DB, scoreID int64) (string, error) {
	var status string
	err := database.QueryRow(`SELECT status FROM scores WHERE id = ?`, scoreID).Scan(&status)
	if err != nil {
		return "", err
	}
	return status, nil
}

func GetApprovedScoreSum(database *sql.DB, studentID int64) (int, error) {
	var total int
	err := database.QueryRow(`
		SELECT IFNULL(SUM(points), 0)
		FROM scores
		WHERE student_id = ? AND status = 'approved'`, studentID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("ошибка при получении баланса ученика %d: %v", studentID, err)
	}
	return total, nil
}

func GetScoresByPeriod(database *sql.DB, periodID int) ([]models.ScoreWithUser, error) {
	row := database.QueryRow(`SELECT start_date, end_date FROM periods WHERE id = ?`, periodID)

	var startDateStr, endDateStr string
	if err := row.Scan(&startDateStr, &endDateStr); err != nil {
		return nil, err
	}
	startDate, err := time.Parse("02.01.2006", startDateStr)
	if err != nil {
		return nil, err
	}
	endDate, err := time.Parse("02.01.2006", endDateStr)
	if err != nil {
		return nil, err
	}

	// включаем весь последний день
	endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	query := `
	SELECT
		s.name AS student_name,
		s.class_number,
		s.class_letter,
		c.name AS category_label,
		scores.points,
		scores.comment,
		a.name AS added_by_name,
		scores.created_at
	FROM scores
	JOIN users s ON scores.student_id = s.id
	JOIN users a ON scores.created_by = a.id
	JOIN categories c ON scores.category_id = c.id
	WHERE scores.created_at BETWEEN ? AND ? AND scores.status = 'approved'
	ORDER BY scores.created_at ASC;
	`
	rows, err := database.Query(query, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ScoreWithUser

	log.Println("⏬ Получаем строки из запроса ...")

	for rows.Next() {
		var s models.ScoreWithUser
		if err := rows.Scan(&s.StudentName, &s.ClassNumber, &s.ClassLetter, &s.CategoryLabel, &s.Points, &s.Comment, &s.AddedByName, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	log.Printf("✅ Получена запись: %+v", result)
	return result, nil
}

// Для отчёта по ученику
func GetScoresByStudentAndDateRange(database *sql.DB, studentID int64, from, to time.Time) ([]models.ScoreWithUser, error) {
	query := `
	SELECT 
	s.id, s.student_id, s.category_id, s.points, s.type, s.comment,
	s.status, s.approved_by, s.approved_at, s.created_by, s.created_at, s.period_id,
	u.name AS student_name, u.class_number, u.class_letter,
	c.name AS category_label, ua.name AS added_by_name

	FROM scores s
	JOIN users u ON u.id = s.student_id
	JOIN users ua ON ua.id = s.created_by
	JOIN categories c ON c.id = s.category_id
	WHERE s.student_id = ? AND s.created_at BETWEEN ? AND ? AND s.status = 'approved'
	ORDER BY s.created_at
	`
	rows, err := database.Query(query, studentID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ScoreWithUser

	log.Println("⏬ Получаем строки из запроса ...")

	for rows.Next() {
		var s models.ScoreWithUser
		err := rows.Scan(
			&s.ID,
			&s.StudentID,
			&s.CategoryID,
			&s.Points,
			&s.Type,
			&s.Comment,
			&s.Status,
			&s.ApprovedBy,
			&s.ApprovedAt,
			&s.CreatedBy,
			&s.CreatedAt,
			&s.PeriodID,
			&s.StudentName,
			&s.ClassNumber,
			&s.ClassLetter,
			&s.CategoryLabel,
			&s.AddedByName,
		)

		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	log.Printf("✅ Получена запись: %+v", result)
	return result, nil
}

// Для отчёта по классу
func GetScoresByClassAndDateRange(database *sql.DB, classNumber int, classLetter string, from, to time.Time) ([]models.ScoreWithUser, error) {
	query := `
	SELECT 
	s.id, s.student_id, s.category_id, s.points, s.type, s.comment,
	s.status, s.approved_by, s.approved_at, s.created_by, s.created_at, s.period_id,
	u.name AS student_name, u.class_number, u.class_letter,
	c.name AS category_label, ua.name AS added_by_name

	FROM scores s
	JOIN users u ON u.id = s.student_id
	JOIN users ua ON ua.id = s.created_by
	JOIN categories c ON c.id = s.category_id
	WHERE u.role = 'student' AND u.class_number = ? AND u.class_letter = ? AND s.created_at BETWEEN ? AND ? AND s.status = 'approved'
	ORDER BY u.name
	`
	rows, err := database.Query(query, classNumber, classLetter, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ScoreWithUser

	log.Println("⏬ Получаем строки из запроса ...")

	for rows.Next() {
		var s models.ScoreWithUser
		err := rows.Scan(
			&s.ID,
			&s.StudentID,
			&s.CategoryID,
			&s.Points,
			&s.Type,
			&s.Comment,
			&s.Status,
			&s.ApprovedBy,
			&s.ApprovedAt,
			&s.CreatedBy,
			&s.CreatedAt,
			&s.PeriodID,
			&s.StudentName,
			&s.ClassNumber,
			&s.ClassLetter,
			&s.CategoryLabel,
			&s.AddedByName,
		)

		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	log.Printf("✅ Получена запись: %+v", result)
	return result, nil
}

// Для отчёта по школе
func GetScoresByDateRange(database *sql.DB, from, to time.Time) ([]models.ScoreWithUser, error) {
	query := `
	SELECT 
	s.id, s.student_id, s.category_id, s.points, s.type, s.comment,
	s.status, s.approved_by, s.approved_at, s.created_by, s.created_at, s.period_id,
	u.name AS student_name, u.class_number, u.class_letter,
	c.name AS category_label, ua.name AS added_by_name

	FROM scores s
	JOIN users u ON u.id = s.student_id
	JOIN users ua ON ua.id = s.created_by
	JOIN categories c ON c.id = s.category_id
	JOIN users a ON a.id = s.created_by
	WHERE u.role = 'student' AND s.created_at BETWEEN ? AND ? AND s.status = 'approved'
	ORDER BY s.created_at
	`
	rows, err := database.Query(query, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ScoreWithUser

	log.Println("⏬ Получаем строки из запроса ...")

	for rows.Next() {
		var s models.ScoreWithUser
		err := rows.Scan(
			&s.ID,
			&s.StudentID,
			&s.CategoryID,
			&s.Points,
			&s.Type,
			&s.Comment,
			&s.Status,
			&s.ApprovedBy,
			&s.ApprovedAt,
			&s.CreatedBy,
			&s.CreatedAt,
			&s.PeriodID,
			&s.StudentName,
			&s.ClassNumber,
			&s.ClassLetter,
			&s.CategoryLabel,
			&s.AddedByName,
		)

		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	log.Printf("✅ Получена запись: %+v", result)
	return result, nil
}

func GetScoresByStudentAndPeriod(database *sql.DB, selectedStudentID int64, periodID int) ([]models.ScoreWithUser, error) {
	row := database.QueryRow(`SELECT start_date, end_date FROM periods WHERE id = ?`, periodID)

	var startDateStr, endDateStr string
	if err := row.Scan(&startDateStr, &endDateStr); err != nil {
		return nil, err
	}
	startDate, err := time.Parse("02.01.2006", startDateStr)
	if err != nil {
		return nil, err
	}
	endDate, err := time.Parse("02.01.2006", endDateStr)
	if err != nil {
		return nil, err
	}

	// включаем весь последний день
	endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	query := `
	SELECT 
	s.id, s.student_id, s.category_id, s.points, s.type, s.comment,
	s.status, s.approved_by, s.approved_at, s.created_by, s.created_at, s.period_id,
	u.name AS student_name, u.class_number, u.class_letter,
	c.name AS category_label, ua.name AS added_by_name

	FROM scores s
	JOIN users u ON u.id = s.student_id
	JOIN users ua ON ua.id = s.created_by
	JOIN categories c ON c.id = s.category_id
	WHERE s.student_id = ? AND s.created_at BETWEEN ? AND ? AND s.status = 'approved'
	ORDER BY s.created_at
	`
	rows, err := database.Query(query, selectedStudentID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ScoreWithUser

	log.Println("⏬ Получаем строки из запроса ...")

	for rows.Next() {
		var s models.ScoreWithUser
		err := rows.Scan(
			&s.ID,
			&s.StudentID,
			&s.CategoryID,
			&s.Points,
			&s.Type,
			&s.Comment,
			&s.Status,
			&s.ApprovedBy,
			&s.ApprovedAt,
			&s.CreatedBy,
			&s.CreatedAt,
			&s.PeriodID,
			&s.StudentName,
			&s.ClassNumber,
			&s.ClassLetter,
			&s.CategoryLabel,
			&s.AddedByName,
		)

		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	log.Printf("✅ Получена запись: %+v", result)
	return result, nil
}

func GetScoresByClassAndPeriod(database *sql.DB, classNumber int64, classLetter string, periodID int64) ([]models.ScoreWithUser, error) {
	row := database.QueryRow(`SELECT start_date, end_date FROM periods WHERE id = ?`, periodID)

	var startDateStr, endDateStr string
	if err := row.Scan(&startDateStr, &endDateStr); err != nil {
		return nil, err
	}
	startDate, err := time.Parse("02.01.2006", startDateStr)
	if err != nil {
		return nil, err
	}
	endDate, err := time.Parse("02.01.2006", endDateStr)
	if err != nil {
		return nil, err
	}

	// включаем весь последний день
	endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	query := `
	SELECT 
	s.id, s.student_id, s.category_id, s.points, s.type, s.comment,
	s.status, s.approved_by, s.approved_at, s.created_by, s.created_at, s.period_id,
	u.name AS student_name, u.class_number, u.class_letter,
	c.name AS category_label, ua.name AS added_by_name

	FROM scores s
	JOIN users u ON u.id = s.student_id
	JOIN users ua ON ua.id = s.created_by
	JOIN categories c ON c.id = s.category_id
	WHERE u.class_number = ? AND u.class_letter = ? AND s.created_at BETWEEN ? AND ? AND s.status = 'approved'
	ORDER BY u.name
	`
	rows, err := database.Query(query, classNumber, classLetter, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.ScoreWithUser

	log.Println("⏬ Получаем строки из запроса ...")

	for rows.Next() {
		var s models.ScoreWithUser
		err := rows.Scan(
			&s.ID,
			&s.StudentID,
			&s.CategoryID,
			&s.Points,
			&s.Type,
			&s.Comment,
			&s.Status,
			&s.ApprovedBy,
			&s.ApprovedAt,
			&s.CreatedBy,
			&s.CreatedAt,
			&s.PeriodID,
			&s.StudentName,
			&s.ClassNumber,
			&s.ClassLetter,
			&s.CategoryLabel,
			&s.AddedByName,
		)

		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	log.Printf("✅ Получена запись: %+v", result)
	return result, nil
}
