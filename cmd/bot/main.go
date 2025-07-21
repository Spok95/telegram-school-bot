package main

import (
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"log"
	"os"
	"strings"
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
		if update.CallbackQuery != nil {
			data := update.CallbackQuery.Data
			if strings.HasPrefix(data, "role_") {
				handlers.HandleRoleInline(bot, database, update.CallbackQuery)
				continue
			}
			if strings.HasPrefix(data, "approve_") || strings.HasPrefix(data, "reject_") {
				handlers.HandlePendingRoleCallback(bot, database, update.CallbackQuery)
				continue
			}
			if strings.HasPrefix(data, "addscore_student_") {
				handlers.HandleAddScoreCallback(bot, database, update.CallbackQuery)
				continue
			}
			if strings.HasPrefix(data, "addscore_category_") {
				handlers.HandleAddScoreCategory(bot, database, update.CallbackQuery)
				continue
			}
			if strings.HasPrefix(data, "addscore_level_") {
				handlers.HandleAddScoreLevel(bot, database, update.CallbackQuery)
				continue
			}
			if data == "addscore_confirm" {
				handlers.HandleAddScoreConfirmCallback(bot, database, update.CallbackQuery)
				continue
			}
			if data == "addscore_cancel" {
				handlers.HandleAddScoreCancelCallback(bot, update.CallbackQuery)
				continue
			}
			bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "⚠️ Неизвестная команда. Используйте /start"))
			continue
		}

		if update.Message != nil {
			if session, ok := handlers.AuthFSMGetSession(update.Message.From.ID); ok {
				switch session.State {
				case handlers.AuthStateFIO:
					handlers.HandleFIO(bot, database, update.Message)
					continue
				case handlers.AuthStateClass:
					handlers.HandleClass(bot, database, update.Message)
					continue
				case handlers.AuthStateChild:
					handlers.HandleChild(bot, database, update.Message)
					continue
				}
			}
		} else {
			continue
		}

		state := handlers.GetAddScoreState(update.Message.Chat.ID)
		if state != nil {
			switch state.Step {
			case handlers.StepValue:
				handlers.HandleAddScoreValue(bot, database, update.Message)
				continue
			case handlers.StepComment:
				handlers.HandleAddScoreComment(bot, database, update.Message)
				continue
			}
		}

		switch update.Message.Text {
		case "/start":
			handlers.HandleStart(bot, database, update.Message)
		case "/setrole":
			handlers.HandleSetRoleRequest(bot, database, update.Message)
		case "/pending_roles":
			handlers.HandlePendingRoles(bot, database, update.Message)
		case "/addscore", "➕ Начислить баллы":
			go handlers.HandleAddScore(bot, database, update.Message)
		case "/myscore", "📊 Мой рейтинг":
			handlers.HandleMyScore(bot, database, update.Message)
		case "📊 Рейтинг ребёнка":
			handlers.HandleMyScore(bot, database, update.Message)
		default:
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "⚠️ Неизвестная команда. Используйте /start")
			bot.Send(msg)
		}
	}
}
