package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/testutil/testdb"
)

func TestPeriods_ActiveSelection(t *testing.T) {
	h, err := testdb.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	now := time.Now()

	past := models.Period{
		Name:      "прошлый",
		StartDate: now.AddDate(0, -2, 0),
		EndDate:   now.AddDate(0, -1, 0),
	}
	cur := models.Period{
		Name:      "текущий",
		StartDate: now.AddDate(0, 0, -1),
		EndDate:   now.AddDate(0, 1, 0),
	}

	if _, err := db.CreatePeriod(h.DB, past); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePeriod(h.DB, cur); err != nil {
		t.Fatal(err)
	}
	if err := db.SetActivePeriod(h.DB); err != nil {
		t.Fatal(err)
	}

	ap, err := db.GetActivePeriod(h.DB)
	if err != nil {
		t.Fatal(err)
	}
	if ap == nil || ap.Name != "текущий" {
		t.Fatalf("ожидали активный 'текущий', получили %#v", ap)
	}
}
