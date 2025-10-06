package fsmutil

import (
	"strings"
	"sync"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// pending — простая защита от повторной обработки "тяжёлых" действий.
// Ключ — chatID; значение — произвольный ключ контекста (например "export:school" или "addscore").
var pending = struct {
	mu sync.Mutex
	m  map[int64]string
}{
	m: make(map[int64]string),
}

// SetPending помечает чат как "в обработке" для ключа key.
// Возвращает false, если уже что-то обрабатывается (т.е. нельзя запускать ещё одно действие).
func SetPending(chatID int64, key string) bool {
	pending.mu.Lock()
	defer pending.mu.Unlock()

	if _, ok := pending.m[chatID]; ok {
		return false
	}
	pending.m[chatID] = key
	return true
}

// ClearPending снимает флаг "в обработке", если ключ совпал.
func ClearPending(chatID int64, key string) {
	pending.mu.Lock()
	defer pending.mu.Unlock()

	if cur, ok := pending.m[chatID]; ok && cur == key {
		delete(pending.m, chatID)
	}
}

// DisableMarkup "гасит" inline‑клавиатуру у сообщения (one‑shot клавиатура).
// Вызываем сразу после обработки callback'а, чтобы предотвратить повторные клики.
func DisableMarkup(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	empty := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)}
	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, empty)
	if _, err := bot.Send(edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// BackCancelRow — готовая строка с кнопками "Назад" и "Отмена".
// Использование: rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
func BackCancelRow(backData, cancelData string) []tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", backData),
		tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", cancelData),
	)
}

// IsCancelText — проверка "текстовой" отмены на шагах, где пользователь вводит текст.
// Поддерживаем: "Отмена", "/cancel", "cancel" (регистр/пробелы игнорим).
func IsCancelText(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "отмена" || s == "/cancel" || s == "cancel"
}

// MustBeActiveForOps — доступ к операциям только для активных пользователей.
func MustBeActiveForOps(u *models.User) bool {
	switch *u.Role {
	case models.Teacher, models.Administration, models.Admin:
		return u.IsActive
	case models.Parent, models.Student:
		return u.IsActive
	default:
		return true
	}
}
