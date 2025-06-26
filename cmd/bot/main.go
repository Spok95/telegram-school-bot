package main

import (
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"log"
	"os"
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
	database, err := db.Init()
	if err != nil {
		log.Fatalf("Ошибка подключения к БД: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatal("Миграция не удалась:", err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Маршрутизация команд
	for update := range updates {
		if update.Message == nil {
			continue
		}
		if update.CallbackQuery != nil {
			handlers.HandleRoleCallback(bot, database, update.CallbackQuery)
			handlers.HandlePendingRoleCallback(bot, database, update.CallbackQuery)
			continue
		}

		switch update.Message.Command() {
		case "start":
			handlers.HandleStart(bot, database, update.Message)
		case "setrole":
			handlers.HandleSetRoleRequest(bot, database, update.Message)
		case "pending_roles":
			handlers.HandlePendingRoles(bot, database, update.Message)
		case "myscore":
			handlers.HandleMyScore(bot, database, update.Message)
		default:
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "⚠️ Неизвестная команда. Используйте /start")
			bot.Send(msg)
		}
	}
}
