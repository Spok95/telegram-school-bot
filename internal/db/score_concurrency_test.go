//go:build testutil
// +build testutil

package db_test

import (
	"context"
	"database/sql"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/testutil/testdb"
)

func TestAddScore_Parallel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	h, err := testdb.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	adminID := mustSeedUser(ctx, t, h.DB, "Админ", models.Admin, nil, nil)
	st1ID := mustSeedUser(ctx, t, h.DB, "Ученик 1", models.Student, ptrInt64(11), ptrString("А"))
	st2ID := mustSeedUser(ctx, t, h.DB, "Ученик 2", models.Student, ptrInt64(11), ptrString("А"))

	// Активность по CURRENT_DATE: используем границы дня
	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	end := now.Add(24 * time.Hour)
	if _, err := db.CreatePeriod(ctx, h.DB, models.Period{
		Name: "Тестовый", StartDate: start, EndDate: end,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetActivePeriod(ctx, h.DB); err != nil {
		t.Fatal(err)
	}
	ap, err := db.GetActivePeriod(ctx, h.DB)
	if err != nil {
		t.Fatal(err)
	}
	if ap == nil {
		t.Fatal("active period not set")
	}
	pid := ap.ID

	catID := db.GetCategoryIDByName(ctx, h.DB, "Социальные поступки")

	wg := sync.WaitGroup{}
	errCh := make(chan error, 1000)
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := db.AddScore(ctx, h.DB, models.Score{
				StudentID:  st1ID,
				CategoryID: int64(catID),
				Points:     10,
				Type:       "add",
				Status:     "approved",
				CreatedBy:  adminID,
				CreatedAt:  time.Now().Add(time.Duration(rand.Intn(1000)) * time.Millisecond),
				PeriodID:   &pid,
			}); err != nil {
				errCh <- err
			}
		}()
		go func() {
			defer wg.Done()
			if err := db.AddScore(ctx, h.DB, models.Score{
				StudentID:  st2ID,
				CategoryID: int64(catID),
				Points:     10,
				Type:       "add",
				Status:     "approved",
				CreatedBy:  adminID,
				CreatedAt:  time.Now().Add(time.Duration(rand.Intn(1000)) * time.Millisecond),
				PeriodID:   &pid,
			}); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Errorf("AddScore: %v", e)
	}

	s1, _ := db.GetScoresByStudentAndPeriod(ctx, h.DB, st1ID, int(ap.ID))
	s2, _ := db.GetScoresByStudentAndPeriod(ctx, h.DB, st2ID, int(ap.ID))

	if sumPoints(s1) != 500 || sumPoints(s2) != 500 {
		t.Fatalf("ожидали по 500 баллов, получили %d и %d", sumPoints(s1), sumPoints(s2))
	}
}

func sumPoints(xs []models.ScoreWithUser) int {
	s := 0
	for _, x := range xs {
		s += x.Points
	}
	return s
}

func mustSeedUser(ctx context.Context, t *testing.T, dbx *sql.DB, name string, role models.Role, classNum *int64, classLet *string) int64 {
	t.Helper()
	var id int64
	err := dbx.QueryRowContext(ctx, `
		INSERT INTO users (telegram_id, name, role, class_number, class_letter, confirmed, is_active)
		VALUES (floor(random()*1e9)::bigint, $1, $2, $3, $4, true, true)
		RETURNING id`, name, string(role), classNum, classLet).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ptrInt64(v int64) *int64    { return &v }
func ptrString(v string) *string { return &v }
