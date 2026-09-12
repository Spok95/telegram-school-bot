package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

var (
	ErrRecoveryAccountNotFound  = errors.New("account recovery target not found")
	ErrRecoveryAmbiguousAccount = errors.New("multiple matching accounts found")
	ErrRecoveryTelegramInUse    = errors.New("telegram id is already bound to an account")
	ErrRecoveryPendingExists    = errors.New("pending recovery request already exists")
	ErrRecoveryPendingSame      = errors.New("same pending recovery request already exists")
	ErrRecoveryRequestNotFound  = errors.New("recovery request not found or already processed")
	ErrRecoveryRequestStale     = errors.New("recovery request is stale")
	ErrRecoveryAdminForbidden   = errors.New("admin account recovery is forbidden")
)

type AccountRecoveryLookup struct {
	Role        string
	Name        string
	StudentName string
	ClassNumber int64
	ClassLetter string
}

type AccountRecoveryTarget struct {
	User             models.User
	MatchedStudentID *int64
}

type AccountRecoveryRequest struct {
	ID               int64
	UserID           int64
	MatchedStudentID *int64
	OldTelegramID    int64
	NewTelegramID    int64
	Status           string
	RequestedAt      time.Time
	ReviewedBy       *int64
	ReviewedAt       *time.Time

	Name        string
	Role        string
	Confirmed   bool
	IsActive    bool
	ClassNumber *int64
	ClassLetter *string

	StudentName        *string
	StudentClassNumber *int64
	StudentClassLetter *string
}

func FindAccountRecoveryTarget(ctx context.Context, database *sql.DB, lookup AccountRecoveryLookup) (*AccountRecoveryTarget, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

	role := strings.TrimSpace(lookup.Role)
	if role == string(models.Admin) {
		return nil, ErrRecoveryAdminForbidden
	}

	if role == string(models.Parent) {
		rows, err := database.QueryContext(ctx, `
SELECT DISTINCT ON (p.id) p.id, p.telegram_id, p.name, p.role, p.class_id, p.class_name,
       p.class_number, p.class_letter, p.child_id, p.confirmed, p.is_active, p.deactivated_at,
       s.id
FROM users p
JOIN parents_students ps ON ps.parent_id = p.id
JOIN users s ON s.id = ps.student_id
WHERE p.role = 'parent'
  AND p.confirmed = TRUE
  AND UPPER(BTRIM(p.name)) = UPPER(BTRIM($1))
  AND UPPER(BTRIM(s.name)) = UPPER(BTRIM($2))
  AND s.class_number = $3
  AND UPPER(BTRIM(s.class_letter)) = UPPER(BTRIM($4))
ORDER BY p.id, s.id
LIMIT 2
`, lookup.Name, lookup.StudentName, lookup.ClassNumber, lookup.ClassLetter)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()

		matches := make([]AccountRecoveryTarget, 0, 2)
		for rows.Next() {
			var target AccountRecoveryTarget
			var studentID int64
			if err := rows.Scan(
				&target.User.ID, &target.User.TelegramID, &target.User.Name, &target.User.Role,
				&target.User.ClassID, &target.User.ClassName, &target.User.ClassNumber, &target.User.ClassLetter,
				&target.User.ChildID, &target.User.Confirmed, &target.User.IsActive, &target.User.DeactivatedAt,
				&studentID,
			); err != nil {
				return nil, err
			}
			target.MatchedStudentID = &studentID
			matches = append(matches, target)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		switch len(matches) {
		case 0:
			return nil, ErrRecoveryAccountNotFound
		case 1:
			return &matches[0], nil
		default:
			return nil, ErrRecoveryAmbiguousAccount
		}
	}

	var (
		rows *sql.Rows
		err  error
	)
	switch role {
	case string(models.Student):
		rows, err = database.QueryContext(ctx, `
SELECT id, telegram_id, name, role, class_id, class_name,
       class_number, class_letter, child_id, confirmed, is_active, deactivated_at
FROM users
WHERE role = 'student'
  AND confirmed = TRUE
  AND UPPER(BTRIM(name)) = UPPER(BTRIM($1))
  AND class_number = $2
  AND UPPER(BTRIM(class_letter)) = UPPER(BTRIM($3))
ORDER BY id
LIMIT 2
`, lookup.Name, lookup.ClassNumber, lookup.ClassLetter)
	case string(models.Teacher), string(models.Administration):
		rows, err = database.QueryContext(ctx, `
SELECT id, telegram_id, name, role, class_id, class_name,
       class_number, class_letter, child_id, confirmed, is_active, deactivated_at
FROM users
WHERE role = $1
  AND confirmed = TRUE
  AND UPPER(BTRIM(name)) = UPPER(BTRIM($2))
ORDER BY id
LIMIT 2
`, role, lookup.Name)
	default:
		return nil, fmt.Errorf("unsupported recovery role: %s", role)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	matches := make([]AccountRecoveryTarget, 0, 2)
	for rows.Next() {
		var target AccountRecoveryTarget
		if err := rows.Scan(
			&target.User.ID, &target.User.TelegramID, &target.User.Name, &target.User.Role,
			&target.User.ClassID, &target.User.ClassName, &target.User.ClassNumber, &target.User.ClassLetter,
			&target.User.ChildID, &target.User.Confirmed, &target.User.IsActive, &target.User.DeactivatedAt,
		); err != nil {
			return nil, err
		}
		matches = append(matches, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	switch len(matches) {
	case 0:
		return nil, ErrRecoveryAccountNotFound
	case 1:
		return &matches[0], nil
	default:
		return nil, ErrRecoveryAmbiguousAccount
	}
}

func CreateAccountRecoveryRequest(ctx context.Context, database *sql.DB, target *AccountRecoveryTarget, newTelegramID int64) (*AccountRecoveryRequest, error) {
	if target == nil {
		return nil, ErrRecoveryAccountNotFound
	}
	userID := target.User.ID

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		oldTelegramID int64
		role          string
		confirmed     bool
	)
	if err := tx.QueryRowContext(ctx, `
SELECT telegram_id, role, confirmed
FROM users
WHERE id = $1
FOR UPDATE
`, userID).Scan(&oldTelegramID, &role, &confirmed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecoveryAccountNotFound
		}
		return nil, err
	}
	if role == string(models.Admin) {
		return nil, ErrRecoveryAdminForbidden
	}
	if !confirmed {
		return nil, ErrRecoveryAccountNotFound
	}
	if oldTelegramID == newTelegramID {
		return nil, ErrRecoveryTelegramInUse
	}

	var existingUserID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE telegram_id = $1`, newTelegramID).Scan(&existingUserID)
	if err == nil {
		return nil, ErrRecoveryTelegramInUse
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var existingReq AccountRecoveryRequest
	err = tx.QueryRowContext(ctx, `
SELECT id, user_id, old_telegram_id, new_telegram_id, status, requested_at
FROM account_recovery_requests
WHERE status = 'pending' AND (user_id = $1 OR new_telegram_id = $2)
ORDER BY id
LIMIT 1
`, userID, newTelegramID).Scan(
		&existingReq.ID,
		&existingReq.UserID,
		&existingReq.OldTelegramID,
		&existingReq.NewTelegramID,
		&existingReq.Status,
		&existingReq.RequestedAt,
	)
	if err == nil {
		if existingReq.UserID == userID &&
			existingReq.NewTelegramID == newTelegramID &&
			existingReq.OldTelegramID == oldTelegramID {
			return nil, ErrRecoveryPendingSame
		}
		return nil, ErrRecoveryPendingExists
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var req AccountRecoveryRequest
	if err := tx.QueryRowContext(ctx, `
INSERT INTO account_recovery_requests (user_id, matched_student_id, old_telegram_id, new_telegram_id)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, old_telegram_id, new_telegram_id, status, requested_at
`, userID, target.MatchedStudentID, oldTelegramID, newTelegramID).Scan(
		&req.ID,
		&req.UserID,
		&req.OldTelegramID,
		&req.NewTelegramID,
		&req.Status,
		&req.RequestedAt,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &req, nil
}

func GetAccountRecoveryRequest(ctx context.Context, database *sql.DB, requestID int64) (*AccountRecoveryRequest, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()

	var req AccountRecoveryRequest
	var (
		classNum, studentClassNum              sql.NullInt64
		classLet, studentName, studentClassLet sql.NullString
		matchedStudentID, reviewedBy           sql.NullInt64
		reviewedAt                             sql.NullTime
	)

	err := database.QueryRowContext(ctx, `
SELECT r.id, r.user_id, r.old_telegram_id, r.new_telegram_id, r.status, r.requested_at,
       r.reviewed_by, r.reviewed_at,
       u.name, u.role, u.confirmed, u.is_active, u.class_number, u.class_letter,
       child.name, child.class_number, child.class_letter, r.matched_student_id
FROM account_recovery_requests r
JOIN users u ON u.id = r.user_id
LEFT JOIN users child ON child.id = r.matched_student_id
WHERE r.id = $1
`, requestID).Scan(
		&req.ID,
		&req.UserID,
		&req.OldTelegramID,
		&req.NewTelegramID,
		&req.Status,
		&req.RequestedAt,
		&reviewedBy,
		&reviewedAt,
		&req.Name,
		&req.Role,
		&req.Confirmed,
		&req.IsActive,
		&classNum,
		&classLet,
		&studentName,
		&studentClassNum,
		&studentClassLet,
		&matchedStudentID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecoveryRequestNotFound
		}
		return nil, err
	}

	if classNum.Valid {
		v := classNum.Int64
		req.ClassNumber = &v
	}
	if classLet.Valid {
		v := classLet.String
		req.ClassLetter = &v
	}
	if studentName.Valid {
		v := studentName.String
		req.StudentName = &v
	}
	if studentClassNum.Valid {
		v := studentClassNum.Int64
		req.StudentClassNumber = &v
	}
	if studentClassLet.Valid {
		v := studentClassLet.String
		req.StudentClassLetter = &v
	}
	if matchedStudentID.Valid {
		v := matchedStudentID.Int64
		req.MatchedStudentID = &v
	}
	if reviewedBy.Valid {
		v := reviewedBy.Int64
		req.ReviewedBy = &v
	}
	if reviewedAt.Valid {
		v := reviewedAt.Time
		req.ReviewedAt = &v
	}

	return &req, nil
}

func ApproveAccountRecoveryRequest(ctx context.Context, database *sql.DB, requestID, adminTelegramID int64) (*AccountRecoveryRequest, error) {
	if !IsAdminID(adminTelegramID) {
		return nil, ErrRecoveryAdminForbidden
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var adminUserID int64
	if err := tx.QueryRowContext(ctx, `
SELECT id
FROM users
WHERE telegram_id = $1 AND role = 'admin' AND confirmed = TRUE AND is_active = TRUE
`, adminTelegramID).Scan(&adminUserID); err != nil {
		return nil, ErrRecoveryAdminForbidden
	}

	var req AccountRecoveryRequest
	var currentTelegramID int64
	var role string
	if err := tx.QueryRowContext(ctx, `
SELECT r.id, r.user_id, r.old_telegram_id, r.new_telegram_id, r.status, r.requested_at,
       u.telegram_id, u.role
FROM account_recovery_requests r
JOIN users u ON u.id = r.user_id
WHERE r.id = $1
FOR UPDATE OF r, u
`, requestID).Scan(
		&req.ID,
		&req.UserID,
		&req.OldTelegramID,
		&req.NewTelegramID,
		&req.Status,
		&req.RequestedAt,
		&currentTelegramID,
		&role,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecoveryRequestNotFound
		}
		return nil, err
	}

	if req.Status != "pending" {
		return nil, ErrRecoveryRequestNotFound
	}
	if role == string(models.Admin) {
		return nil, ErrRecoveryAdminForbidden
	}
	if currentTelegramID != req.OldTelegramID {
		return nil, ErrRecoveryRequestStale
	}

	var occupied bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM users
    WHERE telegram_id = $1 AND id <> $2
)
`, req.NewTelegramID, req.UserID).Scan(&occupied); err != nil {
		return nil, err
	}
	if occupied {
		return nil, ErrRecoveryTelegramInUse
	}

	res, err := tx.ExecContext(ctx, `
UPDATE users
SET telegram_id = $1
WHERE id = $2 AND telegram_id = $3
`, req.NewTelegramID, req.UserID, req.OldTelegramID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrRecoveryRequestStale
	}

	res, err = tx.ExecContext(ctx, `
UPDATE account_recovery_requests
SET status = 'approved', reviewed_by = $1, reviewed_at = NOW()
WHERE id = $2 AND status = 'pending'
`, adminUserID, req.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrRecoveryRequestNotFound
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetAccountRecoveryRequest(ctx, database, requestID)
}

func RejectAccountRecoveryRequest(ctx context.Context, database *sql.DB, requestID, adminTelegramID int64) (*AccountRecoveryRequest, error) {
	if !IsAdminID(adminTelegramID) {
		return nil, ErrRecoveryAdminForbidden
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var adminUserID int64
	if err := tx.QueryRowContext(ctx, `
SELECT id
FROM users
WHERE telegram_id = $1 AND role = 'admin' AND confirmed = TRUE AND is_active = TRUE
`, adminTelegramID).Scan(&adminUserID); err != nil {
		return nil, ErrRecoveryAdminForbidden
	}

	var status string
	if err := tx.QueryRowContext(ctx, `
SELECT status
FROM account_recovery_requests
WHERE id = $1
FOR UPDATE
`, requestID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecoveryRequestNotFound
		}
		return nil, err
	}
	if status != "pending" {
		return nil, ErrRecoveryRequestNotFound
	}

	res, err := tx.ExecContext(ctx, `
UPDATE account_recovery_requests
SET status = 'rejected', reviewed_by = $1, reviewed_at = NOW()
WHERE id = $2 AND status = 'pending'
`, adminUserID, requestID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrRecoveryRequestNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetAccountRecoveryRequest(ctx, database, requestID)
}
