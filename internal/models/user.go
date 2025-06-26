package models

type Role string

const (
	Student Role = "student"
	Teacher Role = "teacher"
	Parent  Role = "parent"
	Admin   Role = "admin"
)

type User struct {
	ID          int64   `db:"id"`
	TelegramID  int64   `db:"telegram_id"`
	Name        string  `db:"name"`
	Role        *Role   `db:"role"`
	ClassID     *int64  `db:"class_id"`
	ChildID     *int64  `db:"child_id"`
	PendingRole *string `db:"pending_role"`
	IsActive    bool    `db:"is_active"`
}
