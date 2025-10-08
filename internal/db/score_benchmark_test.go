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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	h, err := testdb.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer h.Close()

	adminID := mustSeedUserB(ctx, b, h.DB, "Админ", models.Admin, nil, nil)
	stID := mustSeedUserB(ctx, b, h.DB, "Бенч", models.Student, ptrInt64(11), ptrString("А"))

	// В проде активный определяется по CURRENT_DATE → делаем период границами текущих суток
	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	end := now.Add(24 * time.Hour)
	if _, err := db.CreatePeriod(ctx, h.DB, models.Period{
		Name: "bench", StartDate: start, EndDate: end,
	}); err != nil {
		b.Fatal(err)
	}
	if err := db.SetActivePeriod(ctx, h.DB); err != nil {
		b.Fatal(err)
	}
	ap, err := db.GetActivePeriod(ctx, h.DB)
	if err != nil {
		b.Fatal(err)
	}
	if ap == nil {
		b.Fatal("active period not set")
	}
	pid := ap.ID

	cat := db.GetCategoryIDByName(ctx, h.DB, "Социальные поступки")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := db.AddScore(ctx, h.DB, models.Score{
				StudentID:  stID,
				CategoryID: int64(cat),
				Points:     1,
				Type:       "add",
				Status:     "approved",
				CreatedBy:  adminID,
				CreatedAt:  time.Now(),
				PeriodID:   &pid,
			}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func mustSeedUserB(ctx context.Context, b *testing.B, dbx *sql.DB, name string, role models.Role, classNum *int64, classLet *string) int64 {
	b.Helper()
	var id int64
	err := dbx.QueryRowContext(ctx, `
		INSERT INTO users (telegram_id, name, role, class_number, class_letter, confirmed, is_active)
		VALUES (floor(random()*1e9)::bigint, $1, $2, $3, $4, true, true)
		RETURNING id`, name, string(role), classNum, classLet).Scan(&id)
	if err != nil {
		b.Fatal(err)
	}
	return id
}
