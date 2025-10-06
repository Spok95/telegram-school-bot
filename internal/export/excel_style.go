package export

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ApplyDefaultExcelFormatting applies:
// - bold header (row 1),
// - auto-filter on row 1,
// - approximate auto-width for all data columns present on the sheet.
func ApplyDefaultExcelFormatting(f *excelize.File, sheet string) error {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return nil
	}

	// Header bold
	if style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}}); err == nil {
		_ = f.SetCellStyle(sheet, "A1", fmt.Sprintf("%s1", columName(cols)), style)
	}

	// Filter on row 1 across all populated columns
	_ = f.AutoFilter(sheet, fmt.Sprintf("A1:%s1", columName(cols)), nil)

	// Auto-fit column widths by content length heuristic
	widths := make([]float64, cols)
	for c := 0; c < cols; c++ {
		widths[c] = 10 // minimal reasonable width
	}
	for rIdx, row := range rows {
		for cIdx := 0; cIdx < cols; cIdx++ {
			var v string
			if cIdx < len(row) {
				v = row[cIdx]
			}
			// Heuristic: Cyrillic chars tend to be wider; add a small multiplier.
			w := float64(visualLen(v)) * 1.1
			if rIdx == 0 {
				// Add buffer for headers
				w += 1.5
			}
			if w > widths[cIdx] {
				if w > 60 {
					w = 60 // cap to avoid overly wide columns
				}
				widths[cIdx] = w
			}
		}
	}
	for i := 0; i < cols; i++ {
		col := columName(i + 1)
		_ = f.SetColWidth(sheet, col, col, widths[i])
	}
	return nil
}

// BuildStudentReportFilename Build human-readable filenames.
func BuildStudentReportFilename(studentName, className, schoolName string, periodTitle string) string {
	base := fmt.Sprintf("Отчёт по ученику — %s — %s — %s — %s.xlsx",
		cleanName(studentName),
		cleanName(className),
		cleanName(schoolName),
		cleanName(periodTitle),
	)
	return sanitizeFileName(base)
}

func BuildClassReportFilename(className, schoolName string, periodTitle string) string {
	base := fmt.Sprintf("Отчёт по классу — %s — %s — %s.xlsx",
		cleanName(className),
		cleanName(schoolName),
		cleanName(periodTitle),
	)
	return sanitizeFileName(base)
}

// Utility helpers

func columName(n int) string {
	// 1 -> A; 27 -> AA
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+(n%26))) + s
		n /= 26
	}
	return s
}

// visualLen approximates text width by counting runes, treating tabs as 4 chars.
func visualLen(s string) int {
	n := 0
	for _, r := range s {
		if r == '\t' {
			n += 4
		} else {
			n += 1
		}
	}
	return n
}

var invalidFileRe = regexp.MustCompile(`[\\/:*?"<>|]+`)

func sanitizeFileName(s string) string {
	// Collapse spaces and remove invalid characters
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = invalidFileRe.ReplaceAllString(s, "_")
	return s
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	return s
}
