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
