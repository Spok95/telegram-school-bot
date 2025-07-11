package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"log"
)

func AddScore(database *sql.DB, score models.Score) error {
	query := `
INSERT INTO scores (
                    student_id, category, points, type, comment, approved, sreated_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`

	_, err := database.Exec(query,
		score.StudentID,
		score.Category,
		score.Points,
		score.Type,
		score.Comment,
		score.Approved,
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
SELECT id, student_id, category, points, type, comment, approved, created_by, created_at
FROM scores WHERE student_id = ? ORDER BY created_at DESC`, studentID)

	if err != nil {
		log.Println("Ошибка при получении истории баллов:", err)
		return nil, err
	}
	defer rows.Close()

	var scores []models.Score
	for rows.Next() {
		var s models.Score
		err := rows.Scan(&s.ID, &s.StudentID, &s.Category, &s.Points, &s.Type, &s.Comment, &s.Approved, &s.CreatedBy, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		scores = append(scores, s)
	}
	return scores, nil
}
