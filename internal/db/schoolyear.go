package db

import (
	"fmt"
	"time"
)

// SchoolYearStart возвращает начало учебного года для заданного момента (1 сентября, 00:00:00).
func SchoolYearStart(t time.Time) time.Time {
	y, m, _ := t.Date()
	loc := t.Location()
	startYear := y
	if m < time.September {
		startYear = y - 1
	}
	return time.Date(startYear, time.September, 1, 0, 0, 0, 0, loc)
}

// SchoolYearEndExclusive возвращает эксклюзивный конец учебного года (1 сентября следующего года, 00:00:00).
func SchoolYearEndExclusive(t time.Time) time.Time {
	start := SchoolYearStart(t)
	return time.Date(start.Year()+1, time.September, 1, 0, 0, 0, 0, t.Location())
}

// SchoolYearBounds — удобная пара границ [from, to) для учебного года момента t.
func SchoolYearBounds(t time.Time) (time.Time, time.Time) {
	return SchoolYearStart(t), SchoolYearEndExclusive(t)
}

// SchoolYearBoundsByStartYear — границы учебного года, начинающегося 1 сентября startYear (например, 2024 → 01.09.2024–01.09.2025).
func SchoolYearBoundsByStartYear(startYear int) (time.Time, time.Time) {
	loc := time.Now().Location()
	from := time.Date(startYear, time.September, 1, 0, 0, 0, 0, loc)
	to := time.Date(startYear+1, time.September, 1, 0, 0, 0, 0, loc)
	return from, to
}

// CurrentSchoolYearStartYear — «год» учебного года (например, для 2025-03-01 → 2024).
func CurrentSchoolYearStartYear(t time.Time) int {
	if t.Month() < time.September {
		return t.Year() - 1
	}
	return t.Year()
}

// SchoolYearLabel форматирует подпись учебного года: "2024–2025".
func SchoolYearLabel(startYear int) string {
	return fmt.Sprintf("%d–%d", startYear, startYear+1)
}
