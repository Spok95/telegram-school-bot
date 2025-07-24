package main

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/bot/auth"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"log"
	"os"
	"strconv"
	"strings"
)

var userFSMRole = make(map[int64]string)

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
			handleCallback(bot, database, update.CallbackQuery)
			continue
		}

		if update.Message != nil {
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

			handleMessage(bot, database, update.Message)
			continue
		}
	}
}

func handleMessage(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := msg.Text

	adminID, _ := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)

	if chatID == adminID && text == "/start" {
		_, err := database.Exec(`INSERT OR REPLACE INTO users (telegram_id, name, role, confirmed) VALUES (?, ?, ?, 1)`,
			chatID, "Администратор", models.Admin)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка авторизации админа."))
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Вы авторизованы как администратор"))

		keyboard := menu.GetRoleMenu(string(models.Admin))
		msg := tgbotapi.NewMessage(chatID, "Добро пожаловать! Выберите действие:")
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
		return
	}

	switch text {
	case "/start":
		var role string
		var confirmed int
		err := database.QueryRow(`SELECT role, confirmed FROM users WHERE telegram_id = ?`, chatID).Scan(&role, &confirmed)
		if err == nil || confirmed == 1 {
			setUserFSMRole(chatID, role)
			keyboard := menu.GetRoleMenu(role)
			msg := tgbotapi.NewMessage(chatID, "Добро пожаловать! Выберите действие:")
			msg.ReplyMarkup = keyboard
			bot.Send(msg)
			//auth.HandleFSMMessage(chatID, "", role, bot, database)
			return
		}
		msg := tgbotapi.NewMessage(chatID, "Выберите роль для регистрации:")
		roles := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Ученик", "reg_student"),
				tgbotapi.NewInlineKeyboardButtonData("Родитель", "reg_parent"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Учитель", "reg_teacher"),
				tgbotapi.NewInlineKeyboardButtonData("Администрация", "reg_administration"),
			),
		)
		msg.ReplyMarkup = roles
		bot.Send(msg)
	case "/addscore", "➕ Начислить баллы":
		go handlers.HandleAddScore(bot, database, msg)
	case "📉 Списать баллы":
		go handlers.HandleAddScore(bot, database, msg)
	case "/myscore", "📊 Мой рейтинг":
		go handlers.HandleMyScore(bot, database, msg)
	case "📊 Рейтинг ребёнка":
		go handlers.HandleMyScore(bot, database, msg)
	default:
		role := getUserFSMRole(chatID)
		if role == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start"))
			return
		}
		auth.HandleFSMMessage(chatID, text, role, bot, database)
	}
}

func handleCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	data := cb.Data
	chatID := cb.Message.Chat.ID

	if strings.HasPrefix(data, "reg_") {
		role := strings.TrimPrefix(data, "reg_")
		setUserFSMRole(chatID, role)
		auth.StartRegistration(chatID, role, bot, database)
		return
	}

	if strings.HasPrefix(data, "confirm_") || strings.HasPrefix(data, "reject_") {
		handlers.HandleAdminCallback(cb, database, bot, chatID)
		return
	}

	if strings.HasPrefix(data, "addscore_student_") {
		handlers.HandleAddScoreCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "addscore_category_") {
		handlers.HandleAddScoreCategory(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "addscore_level_") {
		handlers.HandleAddScoreLevel(bot, database, cb)
		return
	}
	if data == "addscore_confirm" {
		handlers.HandleAddScoreConfirmCallback(bot, database, cb)
		return
	}
	if data == "addscore_cancel" {
		handlers.HandleAddScoreCancelCallback(bot, cb)
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start"))
}

func setUserFSMRole(chatID int64, role string) {
	userFSMRole[chatID] = role
}

func getUserFSMRole(chatID int64) string {
	return userFSMRole[chatID]
}
