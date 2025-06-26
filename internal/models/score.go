package models

import "time"

type Score struct {
	ID        int64     `db:"id"`
	StudentID int64     `db:"student_id"`
	Category  string    `db:"category"`
	Points    int       `db:"points"`
	Type      string    `db:"type"`
	Comment   *string   `db:"comment"`
	Approved  bool      `db:"approved"`
	CreatedBy int64     `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
}
