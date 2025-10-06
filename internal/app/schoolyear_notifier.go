package app

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// В рамках жизни процесса — чтобы не слать дубликаты при многократных тиках
var lastNotifiedStartYear int

// StartSchoolYearNotifier — раз в день в 07:00 локального времени проверяет 1 сентября и шлёт уведомление админам.
func StartSchoolYearNotifier(bot *tgbotapi.BotAPI, database *sql.DB) {
	go func() {
		loc := time.Now().Location()
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, loc)
			if !now.Before(next) {
				next = next.Add(24 * time.Hour)
			}
			time.Sleep(next.Sub(now))

			today := time.Now()
			if today.Month() == time.September && today.Day() == 1 {
				startYear := db.CurrentSchoolYearStartYear(today)
				if lastNotifiedStartYear != startYear {
					ids, err := db.GetAdminTelegramIDs(database)
					if err != nil {
						log.Println("schoolyear notifier:", err)
						continue
					}
					text := fmt.Sprintf(
						"🎓 Начался новый учебный год %s.\n"+
							"Рейтинги в интерфейсе считаются заново; отчёты за прошлые годы доступны в «Экспорт отчёта → 📘 Учебный год».",
						db.SchoolYearLabel(startYear),
					)
					for _, chatID := range ids {
						_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, text))
					}
					lastNotifiedStartYear = startYear
				}
			}
		}
	}()
}
