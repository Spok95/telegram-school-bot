package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Дедупликация: чтобы не слать вторично в тот же учебный год
var lastNotifiedStartYear int

// RunSchoolYearNotifier выполняет ОДНУ проверку и, если сегодня 1 сентября
// и уже позже 07:00 локального времени — шлёт уведомление админам.
// Возвращает первую системную ошибку отправки/БД (для метрики job_errors).
func RunSchoolYearNotifier(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB) error {
	now := time.Now()
	// 1 сентября после 07:00 локального времени
	if now.Month() == time.September && now.Day() == 1 && now.Hour() >= 7 {
		startYear := db.CurrentSchoolYearStartYear(now)
		if lastNotifiedStartYear == startYear {
			return nil // уже уведомляли в этом году
		}

		ids, err := db.GetAdminTelegramIDs(ctx, database)
		if err != nil {
			return err
		}

		text := fmt.Sprintf(
			"🎓 Начался новый учебный год %s.\n"+
				"Рейтинги в интерфейсе считаются заново; отчёты за прошлые годы доступны в «Экспорт отчёта → 📘 Учебный год».",
			db.SchoolYearLabel(startYear),
		)

		var firstErr error
		for _, chatID := range ids {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, text)); err != nil && firstErr == nil {
				firstErr = err
			}
		}

		lastNotifiedStartYear = startYear
		return firstErr
	}
	return nil
}
