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
	ID       int    `db:"id"`
	Name     string `db:"name"`
	Label    string `db:"label"`
	IsActive bool   `db:"is_active"`
}

type ScoreLevel struct {
	ID         int    `db:"id"`
	Value      int    `db:"value"`
	Label      string `db:"label"`
	CategoryID int    `db:"category_id"`
	IsActive   bool   `db:"is_active"`
}

type ScoreWithUser struct {
	ID            int64      `db:"id"`
	StudentID     int64      `db:"student_id"`
	CategoryID    int64      `db:"category_id"`
	CategoryLabel string     `db:"category_label"`
	Points        int        `db:"points"`
	Type          string     `db:"type"`
	Comment       *string    `db:"comment"`
	Status        string     `db:"status"`
	ApprovedBy    *int64     `db:"approved_by"`
	ApprovedAt    *time.Time `db:"approved_at"`
	CreatedBy     int64      `db:"created_by"`
	CreatedAt     *time.Time `db:"created_at"`
	PeriodID      *int64     `db:"period_id"`
	StudentName   string     `db:"student_name"`
	ClassNumber   int        `db:"class_number"`
	ClassLetter   string     `db:"class_letter"`
	AddedByName   string     `db:"added_by_name"`
}
