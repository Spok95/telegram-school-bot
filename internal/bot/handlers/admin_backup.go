package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/backupclient"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/observability"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// HandleAdminBackup — триггерит бэкап в sidecar (файл сохраняется в ./backups)
func HandleAdminBackup(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	user, err := db.GetUserByTelegramID(database, chatID)
	if err != nil || user == nil || user.Role == nil || *user.Role != "admin" {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Только для администратора")); err != nil {
			metrics.HandlerErrors.Inc()
			observability.CaptureErr(err)
		}
		return
	}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⌛ Делаю бэкап базы…")); err != nil {
		metrics.HandlerErrors.Inc()
		observability.CaptureErr(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	path, err := backupclient.TriggerBackup(ctx)
	if err != nil {
		metrics.HandlerErrors.Inc()
		observability.CaptureErr(err)
		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось сделать бэкап: %v", err)))
		return
	}

	_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Готово. Сохранено: "+path))
}
