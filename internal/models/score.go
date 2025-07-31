package models

import "time"

type Score struct {
	ID            int64      `db:"id"`
	StudentID     int64      `db:"student_id"`
	CategoryID    int64      `db:"category_id"`
	CategoryLabel string     `db:"category"`
	Points        int        `db:"points"`
	Type          string     `db:"type"`
	Comment       *string    `db:"comment"`
	Status        string     `db:"status"`
	ApprovedBy    *int64     `db:"approved_by"`
	ApprovedAt    *time.Time `db:"approved_at"`
	CreatedBy     int64      `db:"created_by"`
	CreatedAt     time.Time  `db:"created_at"`
	PeriodID      *int64     `db:"period_id"`
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

type ScoreWithUser struct {
	StudentName   string
	ClassNumber   int
	ClassLetter   string
	CategoryLabel string
	Points        int
	Comment       string
	AddedByName   string
	CreatedAt     time.Time
}
