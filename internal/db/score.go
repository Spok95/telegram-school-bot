package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

func scanScoreWithUserLight(rows *sql.Rows) (models.ScoreWithUser, error) {
	var s models.ScoreWithUser
	err := rows.Scan(
		&s.StudentName,
		&s.ClassNumber,
		&s.ClassLetter,
		&s.CategoryLabel,
		&s.Points,
		&s.Comment,
		&s.AddedByName,
		&s.CreatedAt,
	)
	return s, err
}

// scanScoreWithUserFull полный скан — для выборок по ученику/классу/школе
func scanScoreWithUserFull(rows *sql.Rows) (models.ScoreWithUser, error) {
	var s models.ScoreWithUser
	err := rows.Scan(
		&s.ID, &s.StudentID, &s.CategoryID, &s.Points, &s.Type, &s.Comment,
		&s.Status, &s.ApprovedBy, &s.ApprovedAt, &s.CreatedBy, &s.CreatedAt, &s.PeriodID,
		&s.StudentName, &s.ClassNumber, &s.ClassLetter, &s.CategoryLabel, &s.AddedByName,
	)
	return s, err
}

func AddScoreContext(ctx context.Context, database *sql.DB, score models.Score) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	query := `
INSERT INTO scores (
                    student_id, category_id, points, type, comment, status, approved_by, approved_at, created_by, created_at, period_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);`

	if score.Type == "remove" {
		score.Points = -score.Points
	}

	_, err := database.ExecContext(ctx, query,
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

func AddScore(database *sql.DB, score models.Score) error {
	return AddScoreContext(context.Background(), database, score)
}

func GetPendingScoresContext(ctx context.Context, database *sql.DB) ([]models.Score, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	rows, err := database.QueryContext(ctx, `
		SELECT s.id, s.student_id, s.category_id, c.name AS category_label, s.points, s.type, s.comment, s.created_by
		FROM scores s
		JOIN categories c ON c.id = s.category_id
		WHERE s.status = 'pending'
		ORDER BY s.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

// GetPendingScores возвращает все заявки, ожидающие подтверждения
func GetPendingScores(database *sql.DB) ([]models.Score, error) {
	return GetPendingScoresContext(context.Background(), database)
}

func AddScoreInstantContext(ctx context.Context, database *sql.DB, score models.Score, approvedBy int64, approvedAt time.Time) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

	if score.Type != "add" {
		return fmt.Errorf("AddScoreInstant: поддерживается только type='add'")
	}
	if score.Points == 0 {
		return fmt.Errorf("AddScoreInstant: points не может быть 0")
	}

	// 1) Обязателен активный период
	period, err := GetActivePeriodContext(ctx, database)
	if err != nil {
		return fmt.Errorf("получение активного периода: %w", err)
	}
	if period == nil {
		return fmt.Errorf("нет активного периода")
	}

	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 2) Вставка сразу approved с обязательным period_id
	_, err = tx.ExecContext(ctx, `
		INSERT INTO scores (
			student_id, category_id, points, type, comment,
			status, approved_by, approved_at, created_by, created_at, period_id
		) VALUES ($1,$2,$3,'add',$4,'approved',$5,$6,$7,NOW(),$8)
	`,
		score.StudentID, score.CategoryID, score.Points, score.Comment,
		approvedBy, approvedAt, score.CreatedBy, period.ID,
	)
	if err != nil {
		return err
	}

	// 3) Обновляем коллективный рейтинг класса (+30% от |points|)
	adj := score.Points
	if adj < 0 {
		adj = -adj
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE classes
		SET collective_score = collective_score + $1
		WHERE id = (SELECT class_id FROM users WHERE id = $2)
	`, (adj*30)/100, score.StudentID); err != nil {
		return err
	}

	return tx.Commit()
}

// AddScoreInstant создать начисление сразу approved и обновить коллективный рейтинг
func AddScoreInstant(database *sql.DB, score models.Score, approvedBy int64, approvedAt time.Time) error {
	return AddScoreInstantContext(context.Background(), database, score, approvedBy, approvedAt)
}

func ApproveScoreContext(ctx context.Context, database *sql.DB, scoreID int64, adminID int64, approvedAt time.Time) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var studentID int64
	var points int
	var scoreType string
	var categoryID int64
	err = tx.QueryRowContext(ctx, `SELECT student_id, points, type, category_id FROM scores WHERE id = $1 AND status = 'pending'`, scoreID).Scan(&studentID, &points, &scoreType, &categoryID)
	if err != nil {
		return fmt.Errorf("заявка не найдена: %v", err)
	}

	// Получаем активный период
	activePeriod, err := GetActivePeriodContext(ctx, database)
	var periodID *int64
	if err == nil && activePeriod != nil {
		periodID = &activePeriod.ID
	}

	// Обновляем заявку: статус + period_id
	_, err = tx.ExecContext(ctx, `
		UPDATE scores 
		SET status = 'approved', approved_by = $1, approved_at = $2, period_id = $3 
		WHERE id = $4`,
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
	catName := GetCategoryNameByID(database, int(categoryID))

	// Обновляем коллективный рейтинг
	if scoreType == "add" {
		_, err = tx.ExecContext(ctx, `UPDATE classes SET collective_score = collective_score + $1 WHERE id = (SELECT class_id FROM users WHERE id = $2)`, adjust*30/100, studentID)
		if err != nil {
			return err
		}
	} else if scoreType == "remove" && catName != "Аукцион" {
		_, err = tx.ExecContext(ctx, `UPDATE classes SET collective_score = collective_score - $1 WHERE id = (SELECT class_id FROM users WHERE id = $2)`, adjust*30/100, studentID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ApproveScore подтверждает заявку и обновляет рейтинг ученика и класса
func ApproveScore(database *sql.DB, scoreID int64, adminID int64, approvedAt time.Time) error {
	return ApproveScoreContext(context.Background(), database, scoreID, adminID, approvedAt)
}

func RejectScoreContext(ctx context.Context, database *sql.DB, scoreID int64, adminID int64, rejectedAt time.Time) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	_, err := database.ExecContext(ctx, `UPDATE scores SET status = 'rejected', approved_by = $1, approved_at = $2 WHERE id = $3`, adminID, rejectedAt, scoreID)
	return err
}

func RejectScore(database *sql.DB, scoreID int64, adminID int64, rejectedAt time.Time) error {
	return RejectScoreContext(context.Background(), database, scoreID, adminID, rejectedAt)
}

func GetScoreStatusByIDContext(ctx context.Context, database *sql.DB, scoreID int64) (string, error) {
	var status string
	err := database.QueryRowContext(ctx, `SELECT status FROM scores WHERE id = $1`, scoreID).Scan(&status)
	if err != nil {
		return "", err
	}
	return status, nil
}

// GetScoreStatusByID возвращает статус заявки по ID
func GetScoreStatusByID(database *sql.DB, scoreID int64) (string, error) {
	return GetScoreStatusByIDContext(context.Background(), database, scoreID)
}

func GetApprovedScoreSumContext(ctx context.Context, database *sql.DB, studentID int64) (int, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var total int
	err := database.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(points), 0)
		FROM scores
		WHERE student_id = $1 AND status = 'approved'`, studentID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("ошибка при получении баланса ученика %d: %v", studentID, err)
	}
	return total, nil
}

func GetApprovedScoreSum(database *sql.DB, studentID int64) (int, error) {
	return GetApprovedScoreSumContext(context.Background(), database, studentID)
}

func GetScoresByPeriodContext(ctx context.Context, database *sql.DB, periodID int) ([]models.ScoreWithUser, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	row := database.QueryRowContext(ctx, `SELECT start_date, end_date FROM periods WHERE id = $1`, periodID)

	var startDate, endDate time.Time
	if err := row.Scan(&startDate, &endDate); err != nil {
		return nil, err
	}
	// включаем весь последний день
	endDate = endDate.Add(24*time.Hour - time.Nanosecond)

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
	WHERE s.role = 'student'
	  AND s.class_number IS NOT NULL 
	  AND s.class_letter IS NOT NULL 
	  AND (
	      s.is_active = TRUE
	      OR (s.is_active = FALSE AND $2 <= s.deactivated_at)
	  	)
	  AND scores.created_at BETWEEN $1 AND $2 AND scores.status = 'approved'
	ORDER BY scores.created_at ASC;
	`
	rows, err := database.QueryContext(ctx, query, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.ScoreWithUser
	for rows.Next() {
		s, err := scanScoreWithUserLight(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func GetScoresByPeriod(database *sql.DB, periodID int) ([]models.ScoreWithUser, error) {
	return GetScoresByPeriodContext(context.Background(), database, periodID)
}

func GetScoresByStudentAndDateRangeContext(ctx context.Context, database *sql.DB, studentID int64, from, to time.Time) ([]models.ScoreWithUser, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
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
	WHERE s.student_id = $1 
	  AND (
	      u.is_active = TRUE
	      OR (u.is_active = FALSE AND $3 <= u.deactivated_at)
	  )
	  AND s.created_at BETWEEN $2 AND $3 AND s.status = 'approved'
	ORDER BY s.created_at
	`
	rows, err := database.QueryContext(ctx, query, studentID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.ScoreWithUser
	for rows.Next() {
		s, err := scanScoreWithUserFull(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// GetScoresByStudentAndDateRange Для отчёта по ученику
func GetScoresByStudentAndDateRange(database *sql.DB, studentID int64, from, to time.Time) ([]models.ScoreWithUser, error) {
	return GetScoresByStudentAndDateRangeContext(context.Background(), database, studentID, from, to)
}

func GetScoresByClassAndDateRangeContext(ctx context.Context, database *sql.DB, classNumber int, classLetter string, from, to time.Time) ([]models.ScoreWithUser, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
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
	WHERE u.role = 'student'
	  AND u.class_number = $1 AND u.class_letter = $2
	  AND (
	      u.is_active = TRUE
	      OR (u.is_active = FALSE AND $4 <= u.deactivated_at)
	  )
	  AND s.created_at BETWEEN $3 AND $4 AND s.status = 'approved'
	ORDER BY u.name
	`
	rows, err := database.QueryContext(ctx, query, classNumber, classLetter, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.ScoreWithUser
	for rows.Next() {
		s, err := scanScoreWithUserFull(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// GetScoresByClassAndDateRange Для отчёта по классу
func GetScoresByClassAndDateRange(database *sql.DB, classNumber int, classLetter string, from, to time.Time) ([]models.ScoreWithUser, error) {
	return GetScoresByClassAndDateRangeContext(context.Background(), database, classNumber, classLetter, from, to)
}

func GetScoresByDateRangeContext(ctx context.Context, database *sql.DB, from, to time.Time) ([]models.ScoreWithUser, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
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
	WHERE u.role = 'student'
	  AND (
	      u.is_active = TRUE
	      OR (u.is_active = FALSE AND $2 <= u.deactivated_at)
	  )
	  AND s.created_at BETWEEN $1 AND $2 AND s.status = 'approved'
	ORDER BY s.created_at
	`
	rows, err := database.QueryContext(ctx, query, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.ScoreWithUser
	for rows.Next() {
		s, err := scanScoreWithUserFull(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// GetScoresByDateRange Для отчёта по школе
func GetScoresByDateRange(database *sql.DB, from, to time.Time) ([]models.ScoreWithUser, error) {
	return GetScoresByDateRangeContext(context.Background(), database, from, to)
}

func GetScoresByStudentAndPeriodContext(ctx context.Context, database *sql.DB, selectedStudentID int64, periodID int) ([]models.ScoreWithUser, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	row := database.QueryRowContext(ctx, `SELECT start_date, end_date FROM periods WHERE id = $1`, periodID)

	var startDate, endDate time.Time
	if err := row.Scan(&startDate, &endDate); err != nil {
		return nil, err
	}

	// включаем весь последний день
	endDate = endDate.Add(24*time.Hour - time.Nanosecond)

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
	WHERE s.student_id = $1
		AND (
	      u.is_active = TRUE
	      OR (u.is_active = FALSE AND $3 <= u.deactivated_at)
	      )
		AND s.status = 'approved'
		AND s.created_at BETWEEN $2 AND $3
	ORDER BY s.created_at
	`
	rows, err := database.QueryContext(ctx, query, selectedStudentID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.ScoreWithUser
	for rows.Next() {
		s, err := scanScoreWithUserFull(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func GetScoresByStudentAndPeriod(database *sql.DB, selectedStudentID int64, periodID int) ([]models.ScoreWithUser, error) {
	return GetScoresByStudentAndPeriodContext(context.Background(), database, selectedStudentID, periodID)
}

func GetScoresByClassAndPeriodContext(ctx context.Context, database *sql.DB, classNumber int64, classLetter string, periodID int64) ([]models.ScoreWithUser, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	row := database.QueryRowContext(ctx, `SELECT start_date, end_date FROM periods WHERE id = $1`, periodID)

	var startDate, endDate time.Time
	if err := row.Scan(&startDate, &endDate); err != nil {
		return nil, err
	}

	// включаем весь последний день
	endDate = endDate.Add(24*time.Hour - time.Nanosecond)

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
	WHERE u.role = 'student'
	  AND u.class_number = $1 AND u.class_letter = $2
	  AND (
	      u.is_active = TRUE
	      OR (u.is_active = FALSE AND $4 <= u.deactivated_at)
	  )
	  AND s.created_at BETWEEN $3 AND $4 AND s.status = 'approved'
	ORDER BY u.name
	`
	rows, err := database.QueryContext(ctx, query, classNumber, classLetter, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []models.ScoreWithUser
	for rows.Next() {
		s, err := scanScoreWithUserFull(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func GetScoresByClassAndPeriod(database *sql.DB, classNumber int64, classLetter string, periodID int64) ([]models.ScoreWithUser, error) {
	return GetScoresByClassAndPeriodContext(context.Background(), database, classNumber, classLetter, periodID)
}
