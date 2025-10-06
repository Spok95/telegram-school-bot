package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/app"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers/migrations"
	"github.com/Spok95/telegram-school-bot/internal/config"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/logging"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

func main() {
	// Загрузка переменных окружения
	if err := godotenv.Load(); err != nil {
		log.Println("Не удалось загрузить .env файл, используем переменные окружения")
	}

	// === CONFIG ===
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// === LOGGING ===
	lg, err := logging.Init(cfg.LogLevel, cfg.Env)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer lg.Closer()
	lg.Sugar.Infow("starting bot", "env", cfg.Env)

	// === CONTEXT + GRACEFUL ===
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// === TELEGRAM ===
	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		lg.Sugar.Fatalw("bot init", "err", err)
	}
	bot.Debug = (cfg.Env == "dev")

	// === DB ===
	os.Setenv("DATABASE_URL", cfg.DatabaseURL) // db.MustOpen читает из ENV
	database, err := db.MustOpen()
	if err != nil {
		lg.Sugar.Fatalw("db open", "err", err)
	}
	defer func() { _ = database.Close() }()

	// Включаем встроенные миграции (из embed.FS)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		lg.Sugar.Fatalw("❌ goose dialect", "err", err)
	}
	if err := goose.Up(database, "."); err != nil {
		lg.Sugar.Fatalw("❌ Ошибка миграций: %v", "err", err)
	}

	err = db.SetActivePeriod(database)
	if err != nil {
		log.Println("❌ Ошибка установки активного периода:", err)
	}

	// Авто-уведомление 1 сентября в 07:00
	app.StartSchoolYearNotifier(bot, database)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// === HTTP: /healthz, /metrics ===
	app.StartHTTP(ctx, cfg.HTTPAddr, database)
	lg.Sugar.Infow("http started", "addr", cfg.HTTPAddr)

	// === Фоновые задачи ===
	// app.StartSchoolYearNotifier(bot, database, cfg.Location) // если функция поддерживает tz

	// === MAIN LOOP ===
	for {
		select {
		case <-ctx.Done():
			lg.Sugar.Infow("shutdown requested")
			time.Sleep(300 * time.Millisecond)
			return
		case upd := <-updates:
			metrics.BotUpdates.Inc()

			// callback/message как было — только завернём в recover-защиту
			func() {
				defer func() {
					if r := recover(); r != nil {
						lg.Sugar.Errorw("panic in update", "panic", r)
						metrics.HandlerErrors.Inc()
					}
				}()

				if upd.CallbackQuery != nil {
					app.HandleCallback(bot, database, upd.CallbackQuery)
					return
				}
				if upd.Message != nil {
					app.HandleMessage(bot, database, upd.Message)
					return
				}
			}()
		}
	}
}
