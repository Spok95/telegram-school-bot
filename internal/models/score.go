package models

import "time"

type Score struct {
	ID            int64     `db:"id"`
	StudentID     int64     `db:"student_id"`
	CategoryID    int64     `db:"category_id"`
	CategoryLabel string    `db:"category"`
	Points        int       `db:"points"`
	Type          string    `db:"type"`
	Comment       *string   `db:"comment"`
	Approved      bool      `db:"approved"`
	CreatedBy     int64     `db:"created_by"`
	CreatedAt     time.Time `db:"created_at"`
}

type Category struct {
	ID   int
	Name string
}

type ScoreLevel struct {
	ID         int
	Value      int
	Label      string
	CategoryID int
}
