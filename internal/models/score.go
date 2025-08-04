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
	ID    int
	Name  string
	Label string
}

type ScoreLevel struct {
	ID         int
	Value      int
	Label      string
	CategoryID int
}

type ScoreWithUser struct {
	ID            int64
	StudentID     int64
	CategoryID    int64
	CategoryLabel string
	Points        int
	Type          string
	Comment       *string
	Status        string
	ApprovedBy    *int64
	ApprovedAt    *time.Time
	CreatedBy     int64
	CreatedAt     *time.Time
	PeriodID      *int64
	StudentName   string
	ClassNumber   int
	ClassLetter   string
	AddedByName   string
}
