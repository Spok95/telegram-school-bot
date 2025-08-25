package db_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/testutil/testdb"
)

func BenchmarkAddScore(b *testing.B) {
	h, err := testdb.Start(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer h.Close()

	adminID := mustSeedUserB(b, h.DB, "Админ", models.Admin, nil, nil)
	stID := mustSeedUserB(b, h.DB, "Бенч", models.Student, ptrInt64(11), ptrString("А"))

	now := time.Now()
	if _, err := db.CreatePeriod(h.DB, models.Period{
		Name: "bench", StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour),
	}); err != nil {
		b.Fatal(err)
	}
	if err := db.SetActivePeriod(h.DB); err != nil {
		b.Fatal(err)
	}
	ap, _ := db.GetActivePeriod(h.DB)
	pid := ap.ID

	cat := db.GetCategoryIDByName(h.DB, "Социальные поступки")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = db.AddScore(h.DB, models.Score{
				StudentID:  stID,
				CategoryID: int64(cat),
				Points:     1,
				Type:       "add",
				Status:     "approved",
				CreatedBy:  adminID,
				CreatedAt:  *ptrTime(time.Now()),
				PeriodID:   &pid,
			})
		}
	})
}

func mustSeedUserB(b *testing.B, dbx *sql.DB, name string, role models.Role, classNum *int64, classLet *string) int64 {
	b.Helper()
	var id int64
	err := dbx.QueryRow(`
		INSERT INTO users (telegram_id, name, role, class_number, class_letter, confirmed, is_active)
		VALUES (floor(random()*1e9)::bigint, $1, $2, $3, $4, true, true)
		RETURNING id`, name, string(role), classNum, classLet).Scan(&id)
	if err != nil {
		b.Fatal(err)
	}
	return id
}
