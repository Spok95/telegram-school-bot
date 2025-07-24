package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"log"
)

func AddScore(database *sql.DB, score models.Score) error {
	query := `
INSERT INTO scores (
                    student_id, category_id, points, type, comment, status, approved_by, approved_at, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`

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
WHERE s.student_id = ? AND s.type = 'add' AND s.status = 'approved'
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
