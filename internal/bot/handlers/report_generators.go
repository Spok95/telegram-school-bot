package handlers

import (
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/xuri/excelize/v2"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// 📄 По ученику
func generateStudentReport(scores []models.ScoreWithUser) (string, error) {
	f := excelize.NewFile()
	sheet := "Report"
	f.SetSheetName("Sheet1", sheet)

	// Заголовки
	headers := []string{"ФИО ученика", "Класс", "Категория", "Баллы", "Комментарий", "Кто добавил", "Дата добавления"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		f.SetCellValue(sheet, cell, h)
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
		_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", row), s.CreatedAt.Format("02.01.2006 15:04"))
	}

	// Автосохранение
	filename := fmt.Sprintf("student_report_%d.xlsx", time.Now().Unix())
	path := filepath.Join(os.TempDir(), filename)

	err := f.SaveAs(path)
	return path, err
}

// 🏫 По классу
func generateClassReport(scores []models.ScoreWithUser) (string, error) {
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

		// Учитываем только баллы НЕ из категории "Аукцион"
		if s.CategoryLabel != "Аукцион" {
			studentMap[key].Total += s.Points
		}
	}

	// Вычисляем вклад
	for _, s := range studentMap {
		s.Contribution = s.Total * 30 / 100
	}

	// Сортировка по убыванию
	var groups []studentGroup
	for _, v := range studentMap {
		groups = append(groups, *v)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Total > groups[j].Total
	})

	f := excelize.NewFile()
	sheet := "ClassReport"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"ФИО ученика", "Класс", "Суммарный балл", "Вклад в коллективный рейтинг"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		f.SetCellValue(sheet, cell, h)
	}
	for i, g := range groups {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), g.Name)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), g.Class)
		_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), g.Total)
		_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), g.Contribution)
	}

	filename := fmt.Sprintf("class_report_%d.xlsx", time.Now().Unix())
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
		return classes[i].Total > classes[j].Total
	})

	// Формируем Excel-отчёт
	f := excelize.NewFile()
	sheet := "SchoolReport"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"Класс", "Коллективный рейтинг"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		f.SetCellValue(sheet, cell, h)
	}
	for i, c := range classes {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), c.Name)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), c.Rating)
	}

	filename := fmt.Sprintf("school_report_%d.xlsx", time.Now().Unix())
	path := filepath.Join(os.TempDir(), filename)
	err := f.SaveAs(path)
	return path, err
}
