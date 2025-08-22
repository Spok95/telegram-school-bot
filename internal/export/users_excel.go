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
	f.DeleteSheet("Sheet1")

	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	// автофильтр только в первой строке
	for i, s := range sheets {
		name := s.Title
		if i == 0 {
			f.SetSheetName("Sheet1", name)
		} else {
			f.NewSheet(name)
		}
		// заголовки
		for col, h := range s.Header {
			cell := fmt.Sprintf("%s1", colName(col+1))
			f.SetCellStr(name, cell, h)
		}
		// стиль заголовков + автофильтр
		end := colName(len(s.Header)) + "1"
		_ = f.SetCellStyle(name, "A1", end, bold)
		_ = f.AutoFilter(name, "A1:"+end, nil)

		// строки
		for r, row := range s.Rows {
			for c, val := range row {
				cell := fmt.Sprintf("%s%d", colName(c+1), r+2)
				f.SetCellStr(name, cell, val)
			}
		}
		// эвристическая ширина: по длине заголовка и первых строк
		for c := 1; c <= len(s.Header); c++ {
			max := len(s.Header[c-1])
			for r := 0; r < min(50, len(s.Rows)); r++ {
				if l := len(s.Rows[r][c-1]); l > max {
					max = l
				}
			}
			w := float64(max) * 0.9
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
		s = string('A'+(n%26)) + s
		n /= 26
	}
	return s
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
