package models

import "time"

type Period struct {
	ID        int64     `db:"id"`
	Name      string    `db:"name"`
	StartDate time.Time `db:"start_date"`
	EndDate   time.Time `db:"end_date"`
	IsActive  bool      `db:"is_active"`
}
