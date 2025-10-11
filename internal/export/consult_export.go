package export

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"
)

func ExportConsultationsExcel(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, teacherID int64, from, to time.Time, loc *time.Location, chatID int64) error {
	// достанем классы, где есть слоты учителя в диапазоне
	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT s.class_id
		FROM consult_slots s
		WHERE s.teacher_id = $1 AND s.start_at >= $2 AND s.start_at < $3
		ORDER BY s.class_id
	`, teacherID, from.UTC(), to.UTC())
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	classIDs := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			classIDs = append(classIDs, id)
		}
	}

	f := excelize.NewFile()
	_ = f.DeleteSheet("Sheet1")

	for _, cid := range classIDs {
		class, _ := db.GetClassByID(ctx, database, cid)
		title := fmt.Sprintf("%d%s", class.Number, strings.ToUpper(class.Letter))
		if title == "" {
			title = fmt.Sprintf("class_%d", cid)
		}
		_, _ = f.NewSheet(title)

		header := fmt.Sprintf("Отчёт по классу %s с %s по %s",
			title,
			from.In(loc).Format("02.01.2006"),
			to.In(loc).Format("02.01.2006"),
		)
		_ = f.SetCellValue(title, "A1", header)
		_ = f.SetCellValue(title, "A3", "Дата")
		_ = f.SetCellValue(title, "B3", "Время")
		_ = f.SetCellValue(title, "C3", "ФИО родителя")
		_ = f.SetCellValue(title, "D3", "ФИО ребёнка")

		// строки
		r, err := database.QueryContext(ctx, `
			SELECT s.start_at, s.end_at, p.name as parent_name, ch.name as child_name
			FROM consult_slots s
			LEFT JOIN users p  ON p.id = s.booked_by_id
			LEFT JOIN users ch ON ch.id = p.child_id
			WHERE s.teacher_id = $1 AND s.class_id = $2
			  AND s.start_at >= $3 AND s.start_at < $4
			ORDER BY s.start_at
		`, teacherID, cid, from.UTC(), to.UTC())
		if err != nil {
			continue
		}
		rn := 4
		for r.Next() {
			var start, end time.Time
			var parentName, childName sql.NullString
			_ = r.Scan(&start, &end, &parentName, &childName)
			_ = f.SetCellValue(title, fmt.Sprintf("A%d", rn), start.In(loc).Format("02.01.2006"))
			_ = f.SetCellValue(title, fmt.Sprintf("B%d", rn), fmt.Sprintf("%s–%s", start.In(loc).Format("15:04"), end.In(loc).Format("15:04")))
			_ = f.SetCellValue(title, fmt.Sprintf("C%d", rn), parentName.String)
			_ = f.SetCellValue(title, fmt.Sprintf("D%d", rn), childName.String)
			rn++
		}
		_ = r.Close()
	}

	path := fmt.Sprintf("/tmp/consult_%d.xlsx", teacherID)
	if err := f.SaveAs(path); err != nil {
		return err
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(path))
	doc.Caption = "Расписание консультаций (по классам, за неделю)"
	if _, err := tg.Send(bot, doc); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return nil
}
