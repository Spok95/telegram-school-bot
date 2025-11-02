package tg

import (
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/observability"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Считаем системными: 5xx, 429, timeout. 400-ки и типичные телеграм-валидации в Sentry не шлём.
func isSystemErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "429") || strings.Contains(s, "502") || strings.Contains(s, "503") || strings.Contains(s, "timeout") {
		return true
	}
	if strings.Contains(s, "Bad Request") ||
		strings.Contains(s, "message is not modified") ||
		strings.Contains(s, "chat not found") ||
		strings.Contains(s, "can't parse entities") {
		return false
	}
	return false
}

func Send(bot *tgbotapi.BotAPI, msg tgbotapi.Chattable) (tgbotapi.Message, error) {
	m, err := bot.Send(msg)
	if isSystemErr(err) {
		observability.CaptureErr(err)
	}
	return m, err
}

func Request(bot *tgbotapi.BotAPI, req tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	r, err := bot.Request(req)
	if isSystemErr(err) {
		observability.CaptureErr(err)
	}
	return r, err
}
