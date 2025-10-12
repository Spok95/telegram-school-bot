package export

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/tg"
)

func ExportConsultationsExcel(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB,
	teacherID int64, from, to time.Time, loc *time.Location, chatID int64) error {

	// Соберём классы этого учителя с слотами в диапазоне
	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT s.class_id
		FROM consult_slots s
		WHERE s.teacher_id = $1
		  AND s.start_at >= $2 AND s.start_at < $3
		ORDER BY s.class_id
	`, teacherID, from.UTC(), to.UTC())
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var classIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		classIDs = append(classIDs, id)
	}

	f := excelize.NewFile()
	_ = f.DeleteSheet("Sheet1")

	if len(classIDs) == 0 {
		const sheet = "Итого"
		_, _ = f.NewSheet(sheet)
		_ = f.SetCellValue(sheet, "A1",
			fmt.Sprintf("Расписание консультаций (%s–%s)",
				from.In(loc).Format("02.01.2006"), to.In(loc).Format("02.01.2006")))
		_ = f.SetCellValue(sheet, "A3", "Данных за период нет")
		tmp, err := os.CreateTemp("", fmt.Sprintf("consult_%d_*.xlsx", teacherID))
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if err := f.SaveAs(tmp.Name()); err != nil {
			return err
		}
		if _, err := tg.Send(bot, tgbotapi.NewDocument(chatID, tgbotapi.FilePath(tmp.Name()))); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return nil
	}

	for _, cid := range classIDs {
		cls, _ := db.GetClassByID(ctx, database, cid)
		sheet := fmt.Sprintf("class_%d", cid)
		if cls != nil {
			sheet = fmt.Sprintf("%d%s", cls.Number, strings.ToUpper(cls.Letter))
		}
		_, _ = f.NewSheet(sheet)

		_ = f.SetCellValue(sheet, "A1",
			fmt.Sprintf("Отчёт по классу %s с %s по %s",
				sheet, from.In(loc).Format("02.01.2006"), to.In(loc).Format("02.01.2006")))
		_ = f.SetCellValue(sheet, "A3", "Дата")
		_ = f.SetCellValue(sheet, "B3", "Время")
		_ = f.SetCellValue(sheet, "C3", "ФИО родителя")
		_ = f.SetCellValue(sheet, "D3", "ФИО ребёнка")

		// ВАЖНО: ребёнка берём через parents_students, ограничивая классом слота
		r, err := database.QueryContext(ctx, `
			SELECT s.start_at, s.end_at,
			       p.name AS parent_name,
			       COALESCE(ch.name, '') AS child_name
			FROM consult_slots s
			JOIN users p ON p.id = s.booked_by_id
			JOIN classes c ON c.id = s.class_id
			LEFT JOIN parents_students ps ON ps.parent_id = s.booked_by_id
			LEFT JOIN users ch ON ch.id = ps.student_id
				AND (
					(ch.class_id = s.class_id)
					OR (ch.class_id IS NULL AND ch.class_number = c.number AND UPPER(ch.class_letter) = UPPER(c.letter))
				)
			WHERE s.teacher_id = $1 AND s.class_id = $2
			  AND s.start_at >= $3 AND s.start_at < $4
			ORDER BY s.start_at
		`, teacherID, cid, from.UTC(), to.UTC())
		if err != nil {
			continue
		}
		row := 4
		for r.Next() {
			var start, end time.Time
			var parentName, childName string
			_ = r.Scan(&start, &end, &parentName, &childName)
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), start.In(loc).Format("02.01.2006"))
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row),
				fmt.Sprintf("%s–%s", start.In(loc).Format("15:04"), end.In(loc).Format("15:04")))
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), parentName)
			_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), childName)
			row++
		}
		_ = r.Close()
	}

	tmp, err := os.CreateTemp("", fmt.Sprintf("consult_%d_*.xlsx", teacherID))
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := f.SaveAs(tmp.Name()); err != nil {
		return err
	}
	if _, err := tg.Send(bot, tgbotapi.NewDocument(chatID, tgbotapi.FilePath(tmp.Name()))); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return nil
}
