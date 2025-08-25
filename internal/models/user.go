package models

import "time"

type Role string

const (
	Admin          Role = "admin"
	Administration Role = "administration"
	Teacher        Role = "teacher"
	Student        Role = "student"
	Parent         Role = "parent"
)

type User struct {
	ID            int64      `db:"id"`
	TelegramID    int64      `db:"telegram_id"`
	Name          string     `db:"name"`
	Role          *Role      `db:"role"`
	ClassID       *int64     `db:"class_id"`
	ClassName     *string    `db:"class_name"`
	ClassNumber   *int64     `db:"class_number"`
	ClassLetter   *string    `db:"class_letter"`
	ChildID       *int64     `db:"child_id"`
	Confirmed     bool       `db:"confirmed"`
	IsActive      bool       `db:"is_active"`
	DeactivatedAt *time.Time `db:"deactivated_at"`
}

type Class struct {
	ID              int64  `db:"id"`
	Name            string `db:"name"`
	Number          int64  `db:"number"`
	Letter          string `db:"letter"`
	CollectiveScore int64  `db:"collective_score"`
}
