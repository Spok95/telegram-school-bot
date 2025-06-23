package models

import "time"

type ScoreLog struct {
	ID        int64
	StudentID int64
	Category  string
	Points    int
	Type      string
	Comment   *string
	Approved  bool
	CreatedBy int64
	CreatedAt time.Time
}
