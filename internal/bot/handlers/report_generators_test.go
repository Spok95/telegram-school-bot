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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	h, err := testdb.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	adminID := mustSeedUser(ctx, t, h.DB, "Админ", models.Admin, nil, nil)
	stID := mustSeedUser(ctx, t, h.DB, "Иванов Иван", models.Student, ptrInt64(11), ptrString("А"))

	// В проде активный определяется по CURRENT_DATE (UTC).
	// Делаем широкий UTC-интервал, чтобы накрыть любые TZ и лаги.
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

	// Категории
	regular := db.GetCategoryIDByName(ctx, h.DB, "Внеурочная активность")
	auction := db.GetCategoryIDByName(ctx, h.DB, "Аукцион")

	// +100 обычных
	if err := db.AddScore(ctx, h.DB, models.Score{
		StudentID:  stID,
		CategoryID: int64(regular),
		Points:     100,
		Type:       "add",
		Status:     "approved",
		CreatedBy:  adminID,
		CreatedAt:  time.Now(),
		PeriodID:   &pid,
	}); err != nil {
		t.Fatal(err)
	}
	// -100 аукцион (Type=remove → Points инвертируется внутри)
	if err := db.AddScore(ctx, h.DB, models.Score{
		StudentID:  stID,
		CategoryID: int64(auction),
		Points:     100,
		Type:       "remove",
		Status:     "approved",
		CreatedBy:  adminID,
		CreatedAt:  time.Now(),
		PeriodID:   &pid,
	}); err != nil {
		t.Fatal(err)
	}

	// Достаём историю
	scores, err := db.GetScoresByStudentAndPeriod(ctx, h.DB, stID, int(ap.ID))
	if err != nil {
		t.Fatal(err)
	}

	collective := int64((100 * 30) / 100) // аукцион не входит
	file, err := generateStudentReport(scores, collective, "11А", "Тестовый")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(file) }()

	f, err := excelize.OpenFile(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	// Заголовок последней колонки
	hdr, _ := f.GetCellValue("Report", "H1")
	if hdr != "Коллективный рейтинг класса" {
		t.Fatalf("ожидали H1=Коллективный рейтинг класса, получили %q", hdr)
	}
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
