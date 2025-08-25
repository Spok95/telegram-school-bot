package handlers

import (
	"testing"
	"time"
)

func TestValidateEditDates(t *testing.T) {
	now := time.Now().Truncate(24 * time.Hour)

	t.Run("active_cannot_set_end_before_today", func(t *testing.T) {
		ep := &EditPeriodState{
			IsActive:  true,
			StartDate: now.Add(-24 * time.Hour),
			EndDate:   now.Add(-24 * time.Hour),
		}
		if err := validateEditDates(ep); err == nil {
			t.Fatal("ожидали ошибку для активного периода с окончанием < сегодня")
		}
	})

	t.Run("future_can_change_both", func(t *testing.T) {
		ep := &EditPeriodState{
			IsActive:  false,
			StartDate: now.Add(24 * time.Hour),
			EndDate:   now.Add(48 * time.Hour),
		}
		if err := validateEditDates(ep); err != nil {
			t.Fatalf("не ожидали ошибку: %v", err)
		}
	})

	t.Run("past_cannot_be_changed", func(t *testing.T) {
		ep := &EditPeriodState{
			IsActive:  false,
			StartDate: now.Add(-72 * time.Hour),
			EndDate:   now.Add(-48 * time.Hour),
		}
		if err := validateEditDates(ep); err == nil {
			t.Fatal("ожидали ошибку для прошедшего периода")
		}
	})

	t.Run("end_before_start_invalid", func(t *testing.T) {
		ep := &EditPeriodState{
			IsActive:  false,
			StartDate: now.Add(48 * time.Hour),
			EndDate:   now.Add(24 * time.Hour),
		}
		if err := validateEditDates(ep); err == nil {
			t.Fatal("ожидали ошибку: end < start")
		}
	})
}
