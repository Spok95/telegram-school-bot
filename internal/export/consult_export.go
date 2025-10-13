package export

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"
)

// ConsultationsExcelExport — XLSX-отчёт «Расписание консультаций». Один лист на класс. Колонки: Дата | Время | ФИО родителя | ФИО ребёнка.
func ConsultationsExcelExport(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	database *sql.DB,
	teacherID int64,
	from, to time.Time,
	loc *time.Location,
	chatID int64,
) error {
	// Список классов, где у учителя есть записи за период (берём только ЗАНЯТЫЕ слоты).
	classIDs, err := distinctClassIDs(ctx, database, teacherID, from, to)
	if err != nil {
		log.Printf("[EXPORT] classIDs query failed: %v", err)
		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось сформировать отчёт."))
		return err
	}

	f := excelize.NewFile()

	// Пусто — формируем аккуратную заглушку.
	if len(classIDs) == 0 {
		const sheet = "Итого"
		_ = f.SetSheetName("Sheet1", sheet)
		_ = f.SetCellValue(sheet, "A1",
			fmt.Sprintf("Расписание консультаций (%s — %s)",
				from.In(loc).Format("02.01.2006"), to.In(loc).Format("02.01.2006")))
		_ = f.SetCellValue(sheet, "A3", "Данных за период нет")
		if err := ApplyDefaultExcelFormatting(f, sheet); err != nil {
			log.Printf("[EXPORT] formatting failed: %v", err)
		}
		return saveAndSend(bot, f, chatID, "consultations_empty")
	}

	// Для каждого класса — отдельный лист.
	firstRenamed := false
	for _, cid := range classIDs {
		cls, _ := db.GetClassByID(ctx, database, cid)
		sheet := fmt.Sprintf("class_%d", cid)
		if cls != nil {
			sheet = fmt.Sprintf("%d%s — %s–%s",
				cls.Number, strings.ToUpper(cls.Letter),
				from.In(loc).Format("02.01.2006"), to.In(loc).Format("02.01.2006"))
		}

		// Первый лист у excelize называется "Sheet1" — переименуем его, остальные создаём.
		if !firstRenamed {
			_ = f.SetSheetName("Sheet1", sheet)
			firstRenamed = true
		} else {
			_, _ = f.NewSheet(sheet)
		}

		// Заголовок таблицы (строка 1)
		headers := []string{"Дата", "Время", "ФИО родителя", "ФИО ребёнка"}
		for i, h := range headers {
			cell := fmt.Sprintf("%s1", columnName(i+1))
			_ = f.SetCellValue(sheet, cell, h)
		}

		// Данные: только занятые слоты этого учителя и класса в периоде.
		rows, err := loadBookedRows(ctx, database, teacherID, cid, from, to, loc)
		if err != nil {
			log.Printf("[EXPORT] rows query failed (class_id=%d): %v", cid, err)
			continue
		}
		// Записываем строки
		row := 2
		for _, r := range rows {
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), r.Date)
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), r.TimeRange)
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), r.ParentName)
			_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), r.ChildName)
			row++
		}

		if err := ApplyDefaultExcelFormatting(f, sheet); err != nil {
			log.Printf("[EXPORT] formatting failed on %s: %v", sheet, err)
		}
	}

	return saveAndSend(bot, f, chatID, fmt.Sprintf("consult_%d", teacherID))
}

// --- helpers ---

type consultRow struct {
	Date       string
	TimeRange  string
	ParentName string
	ChildName  string
}

// distinctClassIDs все классы, где учитель имеет ЗАНЯТЫЕ слоты в периоде
func distinctClassIDs(ctx context.Context, dbx *sql.DB, teacherID int64, from, to time.Time) ([]int64, error) {
	q := `
		SELECT DISTINCT s.class_id
		FROM consult_slots s
		WHERE s.teacher_id = $1
		  AND s.start_at >= $2 AND s.start_at < $3
		  AND s.booked_by_id IS NOT NULL
		ORDER BY 1`
	rows, err := dbx.QueryContext(ctx, q, teacherID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// loadBookedRows строки для конкретного класса
func loadBookedRows(ctx context.Context, dbx *sql.DB, teacherID, classID int64, from, to time.Time, loc *time.Location) ([]consultRow, error) {
	// Берём родителя из s.booked_by_id, а ребёнка — того, кто привязан к родителю и учится в этом же классе.
	q := `
	SELECT
		s.start_at, s.end_at,
		p.name as parent_name,
		COALESCE(ch.name, '') as child_name
	FROM consult_slots s
	JOIN users p ON p.id = s.booked_by_id
	JOIN classes c ON c.id = s.class_id
	LEFT JOIN LATERAL (
		SELECT u.name
		FROM parents_students ps
		JOIN users u ON u.id = ps.student_id
		WHERE ps.parent_id = p.id
		  AND (
		      u.class_id = s.class_id
		      OR (u.class_id IS NULL AND u.class_number = c.number AND UPPER(u.class_letter) = UPPER(c.letter))
		  )
		LIMIT 1
	) ch ON TRUE
	WHERE s.teacher_id = $1
	  AND s.class_id = $2
	  AND s.start_at >= $3 AND s.start_at < $4
	  AND s.booked_by_id IS NOT NULL
	ORDER BY s.start_at`
	rows, err := dbx.QueryContext(ctx, q, teacherID, classID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var res []consultRow
	for rows.Next() {
		var start, end time.Time
		var parent, child string
		if err := rows.Scan(&start, &end, &parent, &child); err != nil {
			return nil, err
		}
		res = append(res, consultRow{
			Date:       start.In(loc).Format("02.01.2006"),
			TimeRange:  fmt.Sprintf("%s–%s", start.In(loc).Format("15:04"), end.In(loc).Format("15:04")),
			ParentName: parent,
			ChildName:  child,
		})
	}
	return res, rows.Err()
}

// saveAndSend сохранить и отправить документ
func saveAndSend(bot *tgbotapi.BotAPI, f *excelize.File, chatID int64, base string) error {
	ts := time.Now().Unix()
	filename := fmt.Sprintf("%s_%d.xlsx", base, ts)
	path := filepath.Join(os.TempDir(), filename)
	if err := f.SaveAs(path); err != nil {
		log.Printf("[EXPORT] save failed: %v", err)
		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось сформировать отчёт."))
		return err
	}
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(path))
	doc.Caption = "📘 Расписание консультаций"
	_, err := tg.Send(bot, doc)
	if err != nil {
		metrics.HandlerErrors.Inc()
	}
	return err
}

// columnName Excel column name (1 -> A, 27 -> AA)
func columnName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+(n%26))) + s
		n /= 26
	}
	return s
}
