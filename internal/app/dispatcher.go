package app

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/auth"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleMessage(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := msg.Text
	db.EnsureAdmin(chatID, database, text, bot)
	// Обработка команды /start без проверки регистрации
	if text == "/start" {
		user, err := db.GetUserByTelegramID(database, chatID)
		if err != nil || user == nil || user.Role == nil {
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
			return
		}
		// 🔒 Пользователь зарегистрирован, но неактивен — доступ закрыт, клавиатуру убираем
		if !user.IsActive {
			rm := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
			rm.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			bot.Send(rm)
			return
		}

		// Пользователь уже зарегистрирован
		db.SetUserFSMRole(chatID, string(*user.Role))
		keyboard := menu.GetRoleMenu(string(*user.Role))
		msg := tgbotapi.NewMessage(chatID, "Добро пожаловать! Выберите действие:")
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
		return
	}

	// Все остальные команды требуют регистрации
	user, err := db.GetUserByTelegramID(database, chatID)
	registered := false
	if err == nil || user != nil && user.Role != nil {
		registered = true
	}

	if !registered {
		role := getUserFSMRole(chatID)
		if role != "" {
			auth.HandleFSMMessage(chatID, text, role, bot, database)
			return
		}

		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Вы не зарегистрированы. Пожалуйста, нажмите /start для начала."))
		return
	}
	// 🔒 Глобальная защёлка: неактивным — ни одну команду
	if user != nil && !user.IsActive {
		rm := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
		// на случай, если у пользователя осталась старая клавиатура — уберём
		rm.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
		bot.Send(rm)
		return
	}
	if handlers.GetAddScoreState(chatID) != nil {
		handlers.HandleAddScoreText(bot, database, msg)
		return
	}
	if handlers.GetRemoveScoreState(chatID) != nil {
		handlers.HandleRemoveText(bot, database, msg)
		return
	}
	if handlers.GetSetPeriodState(chatID) != nil {
		handlers.HandleSetPeriodInput(bot, database, msg)
		return
	}
	if handlers.GetAuctionState(chatID) != nil {
		handlers.HandleAuctionText(bot, database, msg)
		return
	}
	if handlers.GetExportState(chatID) != nil {
		handlers.HandleExportText(bot, database, msg)
		return
	}
	if handlers.GetAdminUsersState(chatID) != nil {
		handlers.HandleAdminUsersText(bot, database, msg)
		return
	}
	if handlers.GetCatalogState(chatID) != nil {
		handlers.HandleCatalogText(bot, database, msg)
		return
	}
	if auth.GetAddChildFSMState(chatID) != "" {
		auth.HandleAddChildText(bot, database, msg)
		return
	}

	switch text {
	case "/add_score", "➕ Начислить баллы":
		go handlers.StartAddScoreFSM(bot, database, msg)
	case "/remove_score", "📉 Списать баллы":
		go handlers.StartRemoveScoreFSM(bot, database, msg)
	case "/my_score", "📊 Мой рейтинг":
		go handlers.HandleMyScore(bot, database, msg)
	case "➕ Добавить ребёнка":
		go auth.StartAddChild(bot, database, msg)
	case "📊 Рейтинг ребёнка":
		if *user.Role == models.Parent {
			go handlers.HandleParentRatingRequest(bot, database, chatID, user.ID)
		}
	case "/approvals", "📥 Заявки на баллы":
		if *user.Role == "admin" || *user.Role == "administration" {
			go handlers.ShowPendingScores(bot, database, chatID)
		}
	case "📥 Заявки на авторизацию":
		adminID, _ := strconv.ParseInt(os.Getenv("ADMIN_ID"), 10, 64)
		if chatID == adminID {
			go handlers.ShowPendingUsers(bot, database, chatID)
			go handlers.ShowPendingParentLinks(bot, database, chatID)
		}
	case "/set_period", "📅 Установить период":
		if *user.Role == "admin" {
			go handlers.StartSetPeriodFSM(bot, msg)
		}
	case "/export", "📥 Экспорт отчёта":
		if *user.Role == "admin" || *user.Role == "administration" {
			go handlers.StartExportFSM(bot, database, msg)
		}
	case "👥 Пользователи":
		if *user.Role == "admin" {
			go handlers.StartAdminUsersFSM(bot, msg)
		}
	case "/auction", "🎯 Аукцион":
		go handlers.StartAuctionFSM(bot, database, msg)
	case "🗂 Справочники":
		if *user.Role == "admin" {
			go handlers.StartCatalogFSM(bot, database, msg)
		}
	default:
		role := getUserFSMRole(chatID)
		if role == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start"))
			return
		}
		auth.HandleFSMMessage(chatID, text, role, bot, database)
	}
}

func HandleCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	data := cb.Data
	chatID := cb.Message.Chat.ID

	// 🔒 Глобальная защёлка для inline-кнопок: неактивным всё режем
	// берём пользователя по Telegram ID отправителя колбэка.
	if cb.From != nil {
		if u, err := db.GetUserByTelegramID(database, cb.From.ID); err == nil && u != nil && !u.IsActive {
			// Всегда отвечаем на колбэк, чтобы Telegram "разморозил" UI
			bot.Request(tgbotapi.NewCallback(cb.ID, "Доступ закрыт"))
			// И даём явное сообщение в чат (на случай, если кнопка была из старого меню)
			msg := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
			// Уберём возможную «залипшую» клавиатуру
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			bot.Send(msg)
			return
		}
	}

	log.Printf("CB from %d: %s (msgID=%d)\n", cb.From.ID, cb.Data, cb.Message.MessageID)

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
	if strings.HasPrefix(data, "per_") || data == "per_confirm" {
		handlers.HandleSetPeriodCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "link_confirm_") || strings.HasPrefix(data, "link_reject_") {
		handlers.HandleParentLinkApprovalCallback(cb, bot, database, chatID)
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
	// Student
	if strings.HasPrefix(data, "student_class_num_") ||
		strings.HasPrefix(data, "student_class_letter_") ||
		data == "student_back" || data == "student_cancel" {
		auth.HandleStudentCallback(cb, bot, database)
		return
	}
	if auth.GetAddChildFSMState(chatID) != "" {
		// Назад/Отмена add-child
		if data == "add_child_back" || data == "add_child_cancel" ||
			strings.HasPrefix(data, "parent_class_num_") ||
			strings.HasPrefix(data, "parent_class_letter_") {
			auth.HandleAddChildCallback(bot, database, cb)
			return
		}
	}
	// Parent
	if strings.HasPrefix(data, "parent_class_num_") ||
		strings.HasPrefix(data, "parent_class_letter_") ||
		data == "parent_back" || data == "parent_cancel" {
		auth.HandleParentCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "add_score_category_") ||
		strings.HasPrefix(data, "add_score_level_") ||
		strings.HasPrefix(data, "add_class_") ||
		strings.HasPrefix(data, "add_score_") ||
		strings.HasPrefix(data, "add_score_student_") ||
		strings.HasPrefix(data, "add_confirm:") ||
		data == "add_students_done" ||
		data == "add_select_all_students" ||
		data == "add_back" ||
		data == "add_cancel" {
		handlers.HandleAddScoreCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "remove_category_") ||
		strings.HasPrefix(data, "remove_level_") ||
		strings.HasPrefix(data, "remove_class_") ||
		strings.HasPrefix(data, "remove_score_") ||
		strings.HasPrefix(data, "remove_student_") ||
		data == "remove_students_done" ||
		data == "remove_select_all_students" ||
		data == "remove_back" ||
		data == "remove_cancel" {
		handlers.HandleRemoveCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "export_type_") ||
		strings.HasPrefix(data, "export_period_") ||
		strings.HasPrefix(data, "export_mode_") ||
		strings.HasPrefix(data, "export_class_number_") ||
		strings.HasPrefix(data, "export_class_letter_") ||
		strings.HasPrefix(data, "export_select_student_") ||
		data == "export_students_done" ||
		data == "export_back" ||
		data == "export_cancel" {
		handlers.HandleExportCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "auction_mode_") ||
		strings.HasPrefix(data, "auction_class_number_") ||
		strings.HasPrefix(data, "auction_class_letter_") ||
		strings.HasPrefix(data, "auction_select_student_") ||
		data == "auction_students_done" ||
		data == "auction_back" ||
		data == "auction_cancel" {
		handlers.HandleAuctionCallback(bot, database, cb)
		return
	}
	if data == "add_another_child_yes" {
		bot.Send(tgbotapi.NewMessage(chatID, "Введите ФИО следующего ребёнка:"))
		msg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}} // мок-сообщение для FSM
		auth.StartAddChild(bot, database, msg)
		return
	}
	if data == "add_another_child_no" {
		msg := tgbotapi.NewMessage(chatID, "Вы вернулись в главное меню.")
		msg.ReplyMarkup = menu.GetRoleMenu("parent")
		bot.Send(msg)
		return
	}
	if strings.HasPrefix(data, "show_rating_student_") {
		idStr := strings.TrimPrefix(data, "show_rating_student_")
		studentID, err := strconv.Atoi(idStr)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка: не удалось определить ученика."))
			return
		}
		handlers.ShowStudentRating(bot, database, chatID, int64(studentID))
		return
	}
	if strings.HasPrefix(data, "admusr_") {
		handlers.HandleAdminUsersCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "catalog_") ||
		data == "catalog_back" || data == "catalog_cancel" {
		handlers.HandleCatalogCallback(bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "exp_users_") {
		user, _ := db.GetUserByTelegramID(database, chatID)

		isAdmin := *user.Role == models.Admin || *user.Role == models.Administration
		switch data {
		case "exp_users_open":
			handlers.ClearExportState(chatID)
			// показать экран параметров экспорта
			handlers.StartExportUsers(bot, database, cb.Message, isAdmin)
		case "exp_users_toggle", "exp_users_gen", "exp_users_cancel", "exp_users_back":
			// обработать кнопки внутри экрана
			handlers.HandleExportUsersCallback(bot, database, cb, isAdmin)
		}
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start"))
}

func getUserFSMRole(chatID int64) string {
	return db.UserFSMRole[chatID]
}
