package main

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/bot/auth"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"log"
	"os"
	"strconv"
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

	err = db.SetActivePeriod(database)
	if err != nil {
		log.Println("❌ Ошибка установки активного периода:", err)
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
			userID := update.Message.From.ID
			if handlers.GetAddScoreState(userID) != nil {
				handlers.HandleAddScoreText(bot, database, update.Message)
				continue
			}
			if handlers.GetRemoveScoreState(userID) != nil {
				handlers.HandleRemoveText(bot, database, update.Message)
				continue
			}
			if handlers.GetSetPeriodState(userID) != nil {
				handlers.HandleSetPeriodInput(bot, database, update.Message)
			}
			if handlers.GetAuctionState(userID) != nil {
				handlers.HandleAuctionText(bot, database, update.Message)
				continue
			}

			handleMessage(bot, database, update.Message)
			continue
		}
	}
}

func handleMessage(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := msg.Text
	db.EnsureAdmin(chatID, database, text, bot)

	adminID, _ := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)
	switch text {
	case "/start":

		var role string
		var confirmed int
		err := database.QueryRow(`SELECT role, confirmed FROM users WHERE telegram_id = ?`, chatID).Scan(&role, &confirmed)
		if err == nil || confirmed == 1 {
			db.SetUserFSMRole(chatID, role)
			keyboard := menu.GetRoleMenu(role)
			msg := tgbotapi.NewMessage(chatID, "Добро пожаловать! Выберите действие:")
			msg.ReplyMarkup = keyboard
			bot.Send(msg)
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
		go handlers.StartAddScoreFSM(bot, database, msg)
	case "/removescore", "📉 Списать баллы":
		go handlers.StartRemoveScoreFSM(bot, database, msg)
	case "/myscore", "📊 Мой рейтинг":
		go handlers.HandleMyScore(bot, database, msg)
	case "📊 Рейтинг ребёнка":
		go handlers.HandleMyScore(bot, database, msg)
	case "/approvals", "📥 Заявки на баллы":
		if chatID == adminID {
			go handlers.ShowPendingScores(bot, database, chatID)
		}
	case "/setperiod", "📅 Установить период":
		role := getUserFSMRole(chatID)
		if role == "admin" || role == "administration" {
			go handlers.StartSetPeriodFSM(bot, database, msg)
		}
	case "/periods":
		isAdmin := chatID == adminID
		go handlers.ShowPeriods(bot, database, chatID, isAdmin)
	case "/export", "📥 Экспорт отчёта":
		user, _ := db.GetUserByTelegramID(database, chatID)
		log.Println("📤 Попытка экспорта. Роль:", *user.Role)
		if *user.Role == "admin" || *user.Role == "administration" {

			log.Println("📊 Роль пользователя:", getUserFSMRole(chatID))

			go handlers.StartExportFSM(bot, msg)
		}
	case "/auction", "🎯 Аукцион":
		go handlers.StartAuctionFSM(bot, database, msg)
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
		db.SetUserFSMRole(chatID, role)
		if role == "parent" {
			auth.StartParentRegistration(chatID, cb.From, bot, database)
		} else {
			auth.StartRegistration(chatID, role, bot, database)
		}
		return
	}

	if strings.HasPrefix(data, "confirm_") ||
		strings.HasPrefix(data, "reject_") {
		handlers.HandleAdminCallback(cb, database, bot, chatID)
		return
	}

	if strings.HasPrefix(data, "score_confirm_") ||
		strings.HasPrefix(data, "score_reject_") {
		handlers.HandleScoreApprovalCallback(cb, bot, database, chatID)
		return
	}

	if strings.HasPrefix(data, "student_class_num_") ||
		strings.HasPrefix(data, "student_class_num_") {
		auth.HandleStudentCallback(cb, bot, database)
		return
	}

	if strings.HasPrefix(data, "student_class_letter_") ||
		strings.HasPrefix(data, "student_class_letter_") {
		auth.HandleStudentCallback(cb, bot, database)
		return
	}

	if strings.HasPrefix(data, "parent_class_num_") {
		numStr := strings.TrimPrefix(data, "parent_class_num_")
		num, err := strconv.Atoi(numStr)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Неверный номер класса"))
			return
		}
		auth.HandleParentClassNumber(chatID, num, bot)
		return
	}

	if strings.HasPrefix(data, "parent_class_letter_") {
		letter := strings.TrimPrefix(data, "parent_class_letter_")
		auth.HandleParentClassLetter(chatID, letter, bot, database)
		return
	}
	if strings.HasPrefix(data, "addscore_category_") ||
		strings.HasPrefix(data, "addscore_level_") ||
		strings.HasPrefix(data, "add_class_") ||
		strings.HasPrefix(data, "addscore_") ||
		strings.HasPrefix(data, "addscore_student_") ||
		data == "add_students_done" {
		handlers.HandleAddScoreCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "remove_category_") ||
		strings.HasPrefix(data, "remove_level_") ||
		strings.HasPrefix(data, "remove_class_") ||
		strings.HasPrefix(data, "removescore_") ||
		strings.HasPrefix(data, "remove_student_") ||
		data == "remove_students_done" {
		handlers.HandleRemoveCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "activate_period_") {
		handlers.HandlePeriodCallback(cb, bot, database)
		return
	}
	if strings.HasPrefix(data, "export_type_") || strings.HasPrefix(data, "export_period_") {
		handlers.HandleExportCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "auction_mode_") ||
		strings.HasPrefix(data, "auction_class_number_") ||
		strings.HasPrefix(data, "auction_class_letter_") ||
		strings.HasPrefix(data, "auction_select_student_") ||
		data == "auction_students_done" {
		handlers.HandleAuctionCallback(bot, database, cb)
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start"))
}

func getUserFSMRole(chatID int64) string {
	return db.UserFSMRole[chatID]
}
