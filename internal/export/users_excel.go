package export

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

type SheetSpec struct {
	Title  string
	Header []string
	Rows   [][]string
}

type UsersWorkbook struct {
	File *excelize.File
}

func NewUsersWorkbook(sheets []SheetSpec) (*UsersWorkbook, error) {
	f := excelize.NewFile()
	// удаляем стандартный Sheet1
	if err := f.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("delete default sheet: %w", err)
	}

	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	// автофильтр только в первой строке
	for i, s := range sheets {
		name := s.Title
		if i == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return nil, fmt.Errorf("rename sheet: %w", err)
			}
		} else {
			if _, err := f.NewSheet(name); err != nil {
				return nil, fmt.Errorf("new sheet: %w", err)
			}
		}
		// заголовки
		for col, h := range s.Header {
			cell := fmt.Sprintf("%s1", colName(col+1))
			if err := f.SetCellStr(name, cell, h); err != nil {
				return nil, fmt.Errorf("set cell %s: %w", cell, err)
			}
		}
		// стиль заголовков + автофильтр
		end := colName(len(s.Header)) + "1"
		_ = f.SetCellStyle(name, "A1", end, bold)
		_ = f.AutoFilter(name, "A1:"+end, nil)

		// строки
		for r, row := range s.Rows {
			for c, val := range row {
				cell := fmt.Sprintf("%s%d", colName(c+1), r+2)
				if err := f.SetCellStr(name, cell, val); err != nil {
					return nil, fmt.Errorf("set cell %s: %w", cell, err)
				}
			}
		}
		// эвристическая ширина: по длине заголовка и первых строк
		for c := 1; c <= len(s.Header); c++ {
			maxim := len(s.Header[c-1])
			for r := 0; r < minim(50, len(s.Rows)); r++ {
				if l := len(s.Rows[r][c-1]); l > maxim {
					maxim = l
				}
			}
			w := float64(maxim) * 0.9
			if w < 12 {
				w = 12
			}
			if w > 40 {
				w = 40
			}
			_ = f.SetColWidth(name, colName(c), colName(c), w)
		}
	}
	return &UsersWorkbook{File: f}, nil
}

func (w *UsersWorkbook) SaveTemp() (string, error) {
	name := fmt.Sprintf("users_%s.xlsx", time.Now().Format("2006-01-02"))
	path := "/tmp/" + name
	return path, w.File.SaveAs(path)
}

// helpers
func colName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+(n%26))) + s
		n /= 26
	}
	return s
}

func minim(a, b int) int {
	if a < b {
		return a
	}
	return b
}
