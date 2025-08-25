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
	h, err := testdb.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	adminID := mustSeedUser(t, h.DB, "Админ", models.Admin, nil, nil)
	st1ID := mustSeedUser(t, h.DB, "Ученик 1", models.Student, ptrInt64(11), ptrString("А"))
	st2ID := mustSeedUser(t, h.DB, "Ученик 2", models.Student, ptrInt64(11), ptrString("А"))

	// Активность по CURRENT_DATE: используем границы дня
	today := time.Now().Truncate(24 * time.Hour)
	if _, err := db.CreatePeriod(h.DB, models.Period{
		Name: "Тестовый", StartDate: today, EndDate: today.Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetActivePeriod(h.DB); err != nil {
		t.Fatal(err)
	}
	ap, _ := db.GetActivePeriod(h.DB)
	if ap == nil {
		t.Fatal("active period not set")
	}
	pid := ap.ID

	catID := db.GetCategoryIDByName(h.DB, "Социальные поступки")

	wg := sync.WaitGroup{}
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = db.AddScore(h.DB, models.Score{
				StudentID:  st1ID,
				CategoryID: int64(catID),
				Points:     10,
				Type:       "add",
				Status:     "approved",
				CreatedBy:  adminID,
				CreatedAt:  time.Now().Add(time.Duration(rand.Intn(1000)) * time.Millisecond),
				PeriodID:   &pid,
			})
		}()
		go func() {
			defer wg.Done()
			_ = db.AddScore(h.DB, models.Score{
				StudentID:  st2ID,
				CategoryID: int64(catID),
				Points:     10,
				Type:       "add",
				Status:     "approved",
				CreatedBy:  adminID,
				CreatedAt:  time.Now().Add(time.Duration(rand.Intn(1000)) * time.Millisecond),
				PeriodID:   &pid,
			})
		}()
	}
	wg.Wait()

	s1, _ := db.GetScoresByStudentAndPeriod(h.DB, st1ID, int(ap.ID))
	s2, _ := db.GetScoresByStudentAndPeriod(h.DB, st2ID, int(ap.ID))

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

func mustSeedUser(t *testing.T, dbx *sql.DB, name string, role models.Role, classNum *int64, classLet *string) int64 {
	t.Helper()
	var id int64
	err := dbx.QueryRow(`
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
