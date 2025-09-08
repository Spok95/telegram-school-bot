package db

import (
	"os"
	"strconv"
	"strings"
)

// EnvAdminIDs читает ADMIN_IDS (CSV/через пробелы) или fallback на ADMIN_ID.
// Возвращает множество (set) ID-шников админов.
func EnvAdminIDs() map[int64]struct{} {
	raw := os.Getenv("ADMIN_IDS")
	if strings.TrimSpace(raw) == "" {
		raw = os.Getenv("ADMIN_ID")
	}
	// Преобразуем любые разделители к запятой
	raw = strings.NewReplacer("\n", ",", "\t", ",", " ", ",", ";", ",").Replace(raw)

	m := make(map[int64]struct{})
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			m[id] = struct{}{}
		}
	}
	return m
}

// IsAdminID — основная проверка «этот chatID — админ?»
func IsAdminID(chatID int64) bool {
	_, ok := EnvAdminIDs()[chatID]
	return ok
}

// AdminIDsSlice — удобно для рассылки всем админам.
func AdminIDsSlice() []int64 {
	m := EnvAdminIDs()
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	return ids
}
