package models

type Role string

const (
	Student Role = "student"
	Teacher Role = "teacher"
	Parent  Role = "parent"
	Admin   Role = "admin"
)

type User struct {
	ID         int64
	TelegramID int64
	Name       string
	Role       Role
	ClassID    int
	ChildID    *int64
}
