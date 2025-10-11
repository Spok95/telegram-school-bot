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
	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/jobs"
	"github.com/Spok95/telegram-school-bot/internal/logging"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/observability"
	"github.com/getsentry/sentry-go"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

var (
	version = "dev"
	commit  = "none"
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
	observability.SetLogger(lg)
	lg.Sugar.Infow("starting bot", "env", cfg.Env)

	closeSentry, err := observability.InitSentry(cfg.SentryDSN, cfg.Env, version+"-"+commit)
	if err != nil {
		lg.Sugar.Warnw("sentry init failed", "err", err)
	}
	defer closeSentry()

	// глобальный guard на панику в main
	defer func() {
		if r := recover(); r != nil {
			sentry.CurrentHub().Recover(r)
			lg.Sugar.Errorw("panic in main", "panic", r)
		}
	}()

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
		lg.Sugar.Fatalw("❌ Ошибка миграций", "err", err)
	}

	err = db.SetActivePeriod(ctx, database)
	if err != nil {
		log.Println("❌ Ошибка установки активного периода:", err)
	}

	// === Фоновые задачи ===
	jr := jobs.New(ctx)

	// Раз в час проверяем «1 сентября после 07:00».
	// Дедуп по lastNotifiedStartYear гарантирует один пуш в год.
	jr.Every(time.Hour, "schoolyear_notifier", func(ctx context.Context) error {
		return app.RunSchoolYearNotifier(ctx, bot, database)
	})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// === HTTP: /healthz, /metrics ===
	app.StartHTTP(ctx, cfg.HTTPAddr, database)
	lg.Sugar.Infow("http started", "addr", cfg.HTTPAddr)

	// === Фоновые задачи ===
	// app.StartSchoolYearNotifier(bot, database, cfg.Location) // если функция поддерживает tz

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
						sentry.CurrentHub().Recover(r)
						lg.Sugar.Errorw("panic in update", "panic", r)
						metrics.HandlerErrors.Inc()
					}
				}()

				// формируем per-update контекст с таймаутом и метаданными
				baseCtx, cancel := ctxutil.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				var chatID int64
				var userID int64
				op := "update"

				if upd.CallbackQuery != nil {
					if upd.CallbackQuery.Message != nil && upd.CallbackQuery.Message.Chat != nil {
						chatID = upd.CallbackQuery.Message.Chat.ID
					}
					if upd.CallbackQuery.From != nil {
						userID = upd.CallbackQuery.From.ID
					}
					op = "callback"
				}
				if upd.Message != nil {
					if upd.Message.Chat != nil {
						chatID = upd.Message.Chat.ID
					}
					if upd.Message.From != nil {
						userID = upd.Message.From.ID
					}
					op = "message"
				}

				// Привязываем метаданные к контексту
				updCtx := ctxutil.WithOp(baseCtx, op)
				if chatID != 0 {
					updCtx = ctxutil.WithChatID(updCtx, chatID)
				}
				if userID != 0 {
					updCtx = ctxutil.WithUserID(updCtx, userID)
				}

				// передаём ctx в app-уровень
				if upd.CallbackQuery != nil {
					app.HandleCallback(updCtx, bot, database, upd.CallbackQuery)
					return
				}
				if upd.Message != nil {
					app.HandleMessage(updCtx, bot, database, upd.Message)
					return
				}
			}()
		}
	}
}
