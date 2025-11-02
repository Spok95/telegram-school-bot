package export

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// ConsultationsExcelExport — XLSX-отчёт «Расписание консультаций». Один лист на класс. Колонки: Дата | Время | ФИО родителя | ФИО ребёнка.
func ConsultationsExcelExport(ctx context.Context, database *sql.DB, teacherID int64, from, _ time.Time, loc *time.Location) (string, error) {
	// НОРМАЛИЗУЕМ ОКНО: с полуночи "from" и до полуночи +14 дней
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	to14 := from.AddDate(0, 0, 14) // именно 14 суток вперёд

	// Собираем классы, по которым у учителя есть слоты в окне (без дублей)
	classRows, err := database.QueryContext(ctx, `
		SELECT DISTINCT c.id, c.number, c.letter
		FROM consult_slots s
		JOIN classes c ON c.id = s.class_id
		WHERE s.teacher_id = $1
		  AND s.start_at >= $2
		  AND s.start_at <  $3
		UNION
		SELECT DISTINCT c2.id, c2.number, c2.letter
		FROM consult_slots s
		JOIN consult_slot_classes csc ON csc.slot_id = s.id
		JOIN classes c2 ON c2.id = csc.class_id
		WHERE s.teacher_id = $1
		  AND s.start_at >= $2
		  AND s.start_at <  $3
		ORDER BY 2, 3
	`, teacherID, from, to14)
	if err != nil {
		return "", err
	}
	defer func() { _ = classRows.Close() }()

	type cls struct {
		ID     int64
		Number int
		Letter string
	}
	var classes []cls
	for classRows.Next() {
		var cl cls
		if err := classRows.Scan(&cl.ID, &cl.Number, &cl.Letter); err != nil {
			return "", err
		}
		classes = append(classes, cl)
	}

	// Готовим xlsx
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Общая функция форматирования листа
	ensureSheet := func(sheet string) error {
		idx, err := f.GetSheetIndex(sheet)
		if err != nil || idx == -1 {
			if _, err := f.NewSheet(sheet); err != nil {
				return err
			}
		}
		_ = f.SetCellValue(sheet, "A1", "Дата")
		_ = f.SetCellValue(sheet, "B1", "Время")
		_ = f.SetCellValue(sheet, "C1", "ФИО родителя")
		_ = f.SetCellValue(sheet, "D1", "ФИО ребёнка")

		_, _ = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
		_ = f.AutoFilter(sheet, "A1:D1", nil)
		_ = f.SetColWidth(sheet, "A", "A", 14)
		_ = f.SetColWidth(sheet, "B", "B", 14)
		_ = f.SetColWidth(sheet, "C", "C", 34)
		_ = f.SetColWidth(sheet, "D", "D", 24)
		return nil
	}

	// Для каждого класса — отдельный лист и выборка БЕЗ дублей
	for _, cl := range classes {
		sheet := fmt.Sprintf("%d%s — %s—%s",
			cl.Number, strings.ToUpper(cl.Letter),
			from.Format("02.01.2006"), to14.Format("02.01.2006"))
		if err := ensureSheet(sheet); err != nil {
			return "", err
		}

		// Ключевой момент:
		//  - берём слоты учителя в окне
		//  - оставляем только ЗАНЯТЫЕ
		//  - и только те, где booked_class_id совпадает с текущим классом
		//  - ребёнка берём по booked_child_id (строго тот, кто выбран при записи)
		rows, err := database.QueryContext(ctx, `
			SELECT 
				s.start_at, s.end_at,
				up.name AS parent_name,
				COALESCE(uc.name, '') AS child_name
			FROM consult_slots s
			JOIN users up ON up.id = s.booked_by_id
			LEFT JOIN users uc ON uc.id = s.booked_child_id
			WHERE s.teacher_id = $1
			  AND s.start_at >= $2
			  AND s.start_at <  $3
			  AND s.booked_by_id IS NOT NULL
			  AND s.booked_class_id = $4
			ORDER BY s.start_at
		`, teacherID, from, to14, cl.ID)
		if err != nil {
			return "", err
		}

		r := 2
		for rows.Next() {
			var start, end time.Time
			var parentName, childName string
			if err := rows.Scan(&start, &end, &parentName, &childName); err != nil {
				_ = rows.Close()
				return "", err
			}
			start = start.In(loc)
			end = end.In(loc)

			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", r), start.Format("02.01.2006"))
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", r), fmt.Sprintf("%s–%s", start.Format("15:04"), end.Format("15:04")))
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", r), parentName)
			_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", r), childName)
			r++
		}
		_ = rows.Close()
	}

	// Активный лист — первый реальный (если есть)
	if len(classes) > 0 {
		first := fmt.Sprintf("%d%s — %s—%s",
			classes[0].Number, strings.ToUpper(classes[0].Letter),
			from.Format("02.01.2006"), to14.Format("02.01.2006"))
		if idx, err := f.GetSheetIndex(first); err == nil && idx >= 0 {
			f.SetActiveSheet(idx)
		}
	}

	// Сохраняем временный файл
	tmp := filepath.Join(os.TempDir(),
		fmt.Sprintf("consult_%d_%d.xlsx", teacherID, time.Now().UnixNano()))
	if err := f.SaveAs(tmp); err != nil {
		return "", err
	}
	return tmp, nil
}

// ConsultationsExcelExportAdmin — XLSX по всем учителям за период.
func ConsultationsExcelExportAdmin(
	ctx context.Context, database *sql.DB,
	from, _ time.Time, loc *time.Location,
) (string, error) {
	to14 := from.AddDate(0, 0, 14)

	type teacherLite struct {
		ID   int64
		Name string
	}

	// Учителя
	rowsT, err := database.QueryContext(ctx, `
		SELECT id, name
		FROM users
		WHERE role = 'teacher' AND confirmed = TRUE AND is_active = TRUE
		ORDER BY LOWER(name)
	`)
	if err != nil {
		return "", err
	}
	defer func() { _ = rowsT.Close() }()

	var teachers []teacherLite
	for rowsT.Next() {
		var t teacherLite
		if err := rowsT.Scan(&t.ID, &t.Name); err != nil {
			return "", err
		}
		teachers = append(teachers, t)
	}
	if err := rowsT.Err(); err != nil {
		return "", err
	}
	if len(teachers) == 0 {
		return "", fmt.Errorf("нет учителей")
	}

	f := excelize.NewFile()
	_ = f.SetSheetName(f.GetSheetName(0), "Сводка")
	_ = f.SetCellValue("Сводка", "A1", "Период")
	_ = f.SetCellValue("Сводка", "B1",
		fmt.Sprintf("%s — %s", from.In(loc).Format("02.01.2006"), to14.In(loc).Format("02.01.2006")))
	_ = f.SetColWidth("Сводка", "A", "B", 28)

	ensureSheet := func(sheet string) error {
		if idx, err := f.GetSheetIndex(sheet); err != nil || idx == -1 {
			if _, err := f.NewSheet(sheet); err != nil {
				return err
			}
		}
		// Порядок колонок: Дата, Время, Класс, ФИО родителя, ФИО ребёнка
		_ = f.SetCellValue(sheet, "A1", "Дата")
		_ = f.SetCellValue(sheet, "B1", "Время")
		_ = f.SetCellValue(sheet, "C1", "Класс")
		_ = f.SetCellValue(sheet, "D1", "ФИО родителя")
		_ = f.SetCellValue(sheet, "E1", "ФИО ребёнка")
		_ = f.AutoFilter(sheet, "A1:E1", nil)
		_ = f.SetColWidth(sheet, "A", "A", 14)
		_ = f.SetColWidth(sheet, "B", "B", 14)
		_ = f.SetColWidth(sheet, "C", "C", 10)
		_ = f.SetColWidth(sheet, "D", "D", 34)
		_ = f.SetColWidth(sheet, "E", "E", 24)
		return nil
	}

	rowSum := 3
	firstDataSheetIdx := -1

	for _, t := range teachers {
		sheet := t.Name

		// Данные по учителю: только реальные брони и конкретный ребёнок/класс
		rows, err := database.QueryContext(ctx, `
			SELECT
				(s.start_at AT TIME ZONE $3) AS st,
				(s.end_at   AT TIME ZONE $3) AS et,
				cls.number, cls.letter,
				up.name AS parent_name,
				uc.name AS child_name
			FROM consult_slots s
			JOIN classes cls ON cls.id = s.booked_class_id
			LEFT JOIN users up ON up.id = s.booked_by_id
			LEFT JOIN users uc ON uc.id = s.booked_child_id
			WHERE s.teacher_id = $1
			  AND s.start_at >= $2 AND s.start_at < $4
			  AND s.booked_by_id   IS NOT NULL
			  AND s.booked_class_id IS NOT NULL
			  AND s.booked_child_id IS NOT NULL
			ORDER BY st ASC, et ASC, cls.number ASC, LOWER(cls.letter) ASC
		`, t.ID, from, loc.String(), to14)
		if err != nil {
			return "", err
		}

		type rec struct {
			Date   string
			Time   string
			Class  string
			Parent string
			Child  string
		}
		var data []rec

		for rows.Next() {
			var st, et time.Time
			var num int
			var letter string
			var parent, child sql.NullString
			if err := rows.Scan(&st, &et, &num, &letter, &parent, &child); err != nil {
				_ = rows.Close()
				return "", err
			}
			if !child.Valid {
				continue
			}
			data = append(data, rec{
				Date:   st.In(loc).Format("02.01.2006"),
				Time:   fmt.Sprintf("%s—%s", st.In(loc).Format("15:04"), et.In(loc).Format("15:04")),
				Class:  fmt.Sprintf("%d%s", num, strings.ToUpper(letter)),
				Parent: parent.String,
				Child:  child.String,
			})
		}
		_ = rows.Close()

		if len(data) == 0 {
			_ = f.SetCellValue("Сводка", fmt.Sprintf("A%d", rowSum), t.Name)
			_ = f.SetCellValue("Сводка", fmt.Sprintf("B%d", rowSum), "нет записей")
			rowSum++
			continue
		}

		if err := ensureSheet(sheet); err != nil {
			return "", err
		}
		r := 2
		for _, v := range data {
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", r), v.Date)
			_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", r), v.Time)
			_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", r), v.Class)
			_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", r), v.Parent)
			_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", r), v.Child)
			r++
		}

		_ = f.SetCellValue("Сводка", fmt.Sprintf("A%d", rowSum), t.Name)
		_ = f.SetCellFormula("Сводка", fmt.Sprintf("B%d", rowSum),
			fmt.Sprintf(`HYPERLINK("#'%s'!A1","лист")`, sheet))
		rowSum++

		if firstDataSheetIdx == -1 {
			if idx, err := f.GetSheetIndex(sheet); err == nil && idx >= 0 {
				firstDataSheetIdx = idx
			}
		}
	}

	if firstDataSheetIdx >= 0 {
		f.SetActiveSheet(firstDataSheetIdx)
	}

	path := filepath.Join(os.TempDir(),
		fmt.Sprintf("consultations_admin_%s.xlsx", time.Now().Format("20060102150405")))
	if err := f.SaveAs(path); err != nil {
		return "", err
	}
	return path, nil
}
