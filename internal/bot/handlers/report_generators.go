package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/export"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/xuri/excelize/v2"
)

// 📄 По ученику
func generateStudentReport(scores []models.ScoreWithUser, collective int64, className string, periodTitle string) (string, error) {
	f := excelize.NewFile()
	sheet := "Report"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return "", err
	}

	// Заголовки
	headers := []string{"ФИО ученика", "Класс", "Категория", "Баллы", "Комментарий", "Кто добавил", "Дата добавления", "Коллективный рейтинг класса"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return "", err
		}
	}
	// Данные
	for i, s := range scores {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), s.StudentName)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("%d%s", s.ClassNumber, s.ClassLetter))
		_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), s.CategoryLabel)
		_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), s.Points)
		if s.Comment != nil {
			_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), *s.Comment)
		} else {
			_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), "")
		}
		_ = f.SetCellValue(sheet, fmt.Sprintf("F%d", row), s.AddedByName)
		if s.CreatedAt != nil {
			_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", row), s.CreatedAt.Format("02.01.2006 15:04"))
		} else {
			_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", row), "")
		}
		_ = f.SetCellValue(sheet, fmt.Sprintf("H%d", row), collective)
	}

	// Автосохранение
	if err := export.ApplyDefaultExcelFormatting(f, sheet); err != nil {
		return "", err
	}
	studentName := ""
	if len(scores) > 0 {
		studentName = scores[0].StudentName
	}
	ts := time.Now().Format("20060102-1504")
	filename := export.BuildStudentReportFilename(studentName, className, periodTitle, ts)
	path := filepath.Join(os.TempDir(), filename)
	err := f.SaveAs(path)
	return path, err
}

// 🏫 По классу
func generateClassReport(scores []models.ScoreWithUser, collective int64, className string, periodTitle string) (string, error) {
	type studentGroup struct {
		Name         string
		Total        int
		Class        string
		Contribution int
	}

	studentMap := make(map[string]*studentGroup)
	for _, s := range scores {
		key := s.StudentName
		if _, exists := studentMap[key]; !exists {
			studentMap[key] = &studentGroup{
				Name:  s.StudentName,
				Class: fmt.Sprintf("%d%s", s.ClassNumber, s.ClassLetter),
			}
		}
		studentMap[key].Total += s.Points
	}

	for _, s := range scores {
		key := s.StudentName
		if s.CategoryLabel != "Аукцион" {
			studentMap[key].Contribution += s.Points
		}
	}

	// Вычисляем вклад
	for _, s := range studentMap {
		s.Contribution = s.Contribution * 30 / 100
	}

	// Сортировка по убыванию
	var groups []studentGroup
	for _, v := range studentMap {
		groups = append(groups, *v)
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})

	f := excelize.NewFile()
	sheet := "ClassReport"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return "", err
	}

	headers := []string{"ФИО ученика", "Класс", "Суммарный балл", "Вклад в коллективный рейтинг", "Коллективный рейтинг класса"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return "", err
		}
	}
	for i, g := range groups {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), g.Name)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), g.Class)
		_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), g.Total)
		_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), g.Contribution)
		_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), collective)
	}

	if err := export.ApplyDefaultExcelFormatting(f, sheet); err != nil {
		return "", err
	}
	ts := time.Now().Format("20060102-1504")
	filename := export.BuildClassReportFilename(className, periodTitle, ts)
	path := filepath.Join(os.TempDir(), filename)
	err := f.SaveAs(path)
	return path, err
}

// 🏫 По школе
func generateSchoolReport(scores []models.ScoreWithUser) (string, error) {
	type classStat struct {
		Name   string
		Total  int
		Rating int
	}
	classMap := make(map[string]*classStat)
	for _, s := range scores {
		classKey := fmt.Sprintf("%d%s", s.ClassNumber, s.ClassLetter)
		// Инициализация структуры по классу
		if _, exists := classMap[classKey]; !exists {
			classMap[classKey] = &classStat{Name: classKey}
		}
		// Учитываем только баллы НЕ из категории "Аукцион"
		if s.CategoryLabel != "Аукцион" {
			classMap[classKey].Total += s.Points
		}
	}

	// Рассчитываем рейтинг как 30% от общей суммы баллов
	for _, class := range classMap {
		class.Rating = class.Total * 30 / 100
	}

	// Сортировка классов по убыванию рейтинга
	var classes []classStat
	for _, v := range classMap {
		classes = append(classes, *v)
	}
	sort.Slice(classes, func(i, j int) bool {
		var numI, numJ int
		var letI, letJ string
		if _, err := fmt.Sscanf(classes[i].Name, "%d%s", &numI, &letI); err != nil {
			return false
		}
		if _, err := fmt.Sscanf(classes[j].Name, "%d%s", &numJ, &letJ); err != nil {
			return false
		}

		if numI != numJ {
			return numI < numJ
		}
		if classes[i].Total != classes[j].Total {
			return classes[i].Total > classes[j].Total
		}
		return letI < letJ
	})

	// Формируем Excel-отчёт
	f := excelize.NewFile()
	sheet := "SchoolReport"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return "", err
	}

	headers := []string{"Класс", "Коллективный рейтинг"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return "", err
		}
	}
	for i, c := range classes {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), c.Name)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), c.Rating)
	}

	if err := export.ApplyDefaultExcelFormatting(f, sheet); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("school_report_%d.xlsx", time.Now().Unix())
	path := filepath.Join(os.TempDir(), filename)
	err := f.SaveAs(path)
	return path, err
}
