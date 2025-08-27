package main

import (
	"log"
	"os"

	"github.com/Spok95/telegram-school-bot/internal/app"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers/migrations"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

func main() {
	// Загрузка переменных окружения
	if err := godotenv.Load(); err != nil {
		log.Println("Не удалось загрузить .env файл, используем переменные окружения")
	}

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		log.Fatal("BOT_TOKEN не задан")
	}

	// Инициализация Telegram бота
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatalf("Ошибка запуска бота: %v", err)
	}
	bot.Debug = true
	log.Printf("Бот запущен как %s", bot.Self.UserName)

	// Инициализация БД через db.Init()
	database, err := db.MustOpen()
	if err != nil {
		log.Fatalf("Ошибка подключения к БД: %v", err)
	}
	defer database.Close()

	// Включаем встроенные миграции (из embed.FS)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("❌ Goose dialect error: %v", err)
	}
	if err := goose.Up(database, "."); err != nil {
		log.Fatalf("❌ Ошибка миграций: %v", err)
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

	// Маршрутизация команд
	for update := range updates {
		if update.CallbackQuery != nil {
			app.HandleCallback(bot, database, update.CallbackQuery)
			continue
		}
		if update.Message != nil {
			app.HandleMessage(bot, database, update.Message)
			continue
		}
	}
}
