package handlers

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/testutil/testdb"
	"github.com/xuri/excelize/v2"
)

func TestGenerateStudentReport(t *testing.T) {
	h, err := testdb.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	adminID := mustSeedUser(t, h.DB, "Админ", models.Admin, nil, nil)
	stID := mustSeedUser(t, h.DB, "Иванов Иван", models.Student, ptrInt64(11), ptrString("А"))

	// В проде активный проверяется по CURRENT_DATE.
	// Делаем период "сегодня 00:00...завтра 00:00", чтобы гарантированно считался активным.
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

	// Категории
	regular := db.GetCategoryIDByName(h.DB, "Внеурочная активность")
	auction := db.GetCategoryIDByName(h.DB, "Аукцион")

	// +100 обычных
	_ = db.AddScore(h.DB, models.Score{
		StudentID:  stID,
		CategoryID: int64(regular),
		Points:     100,
		Type:       "add",
		Status:     "approved",
		CreatedBy:  adminID,
		CreatedAt:  time.Now(),
		PeriodID:   &pid,
	})
	// -100 аукцион (Type=remove → Points инвертируется внутри)
	_ = db.AddScore(h.DB, models.Score{
		StudentID:  stID,
		CategoryID: int64(auction),
		Points:     100,
		Type:       "remove",
		Status:     "approved",
		CreatedBy:  adminID,
		CreatedAt:  time.Now(),
		PeriodID:   &pid,
	})

	// Достаём историю
	scores, err := db.GetScoresByStudentAndPeriod(h.DB, stID, int(ap.ID))
	if err != nil {
		t.Fatal(err)
	}

	collective := int64((100 * 30) / 100) // аукцион не входит
	file, err := generateStudentReport(scores, collective, "11А", "Тестовый")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file)

	f, err := excelize.OpenFile(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Заголовок последней колонки
	hdr, _ := f.GetCellValue("Report", "H1")
	if hdr != "Коллективный рейтинг класса" {
		t.Fatalf("ожидали H1=Коллективный рейтинг класса, получили %q", hdr)
	}
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
