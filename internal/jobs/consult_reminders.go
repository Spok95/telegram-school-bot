package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/app"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/observability"
)

func StartConsultReminderLoop(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB) {
	go runEvery(ctx, time.Minute, func(c context.Context) { process(c, bot, database, "24 hours") })
	go runEvery(ctx, time.Minute, func(c context.Context) { process(c, bot, database, "1 hours") })
}

func runEvery(ctx context.Context, d time.Duration, fn func(context.Context)) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						observability.CaptureErr(fmt.Errorf("panic in reminder job: %v", r))
					}
				}()
				fn(ctx)
			}()
		}
	}
}

func process(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, intervalText string) {
	// 1) Кандидаты на напоминание
	slots, err := db.DueForReminder(ctx, database, intervalText, 100)
	if err != nil {
		observability.CaptureErr(err)
		return
	}
	if len(slots) == 0 {
		return
	}

	// 2) Отправка
	done := make([]int64, 0, len(slots))
	for _, s := range slots {
		if !s.BookedByID.Valid {
			continue
		}
		if err := app.SendConsultReminder(ctx, bot, database, s, intervalText); err != nil {
			observability.CaptureErr(err)
			continue
		}
		done = append(done, s.ID)
	}

	// 3) Пометка
	if len(done) > 0 {
		if err := db.MarkReminded(ctx, database, done, intervalText); err != nil {
			observability.CaptureErr(err)
		}
	}
}
