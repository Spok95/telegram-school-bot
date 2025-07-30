package models

import "time"

type Period struct {
	ID        int64
	Name      string
	StartDate time.Time
	EndDate   time.Time
	IsActive  bool
}
