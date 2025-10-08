package app

import (
	"context"
	"database/sql"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/auth"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var chatLimiter = NewChatLimiter()

func HandleMessage(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	// базовый контекст для обработки входящего сообщения
	ctx = ctxutil.WithChatID(
		ctxutil.WithOp(ctx, "tg.message"),
		chatID,
	)
	text := msg.Text
	db.EnsureAdmin(ctx, chatID, database, text, bot)

	// 🔁 Если активен FSM восстановления БД — делегируем туда любой апдейт (текст/документ)
	if handlers.AdminRestoreFSMActive(chatID) {
		handlers.HandleAdminRestoreMessage(ctx, bot, database, msg)
		return
	}

	// Обработка команды /start без проверки регистрации
	if text == "/start" {
		user, err := db.GetUserByTelegramID(ctx, database, chatID)
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
			if _, err := tg.Send(bot, msg); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		// 🔒 Пользователь зарегистрирован, но неактивен — доступ закрыт, клавиатуру убираем
		if !user.IsActive {
			rm := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
			rm.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			if _, err := tg.Send(bot, rm); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		// положим внутр. userID в контекст (полезно для логов/метрик ниже)
		ctx = ctxutil.WithUserID(ctx, user.ID)

		// Пользователь уже зарегистрирован
		db.SetUserFSMRole(chatID, string(*user.Role))
		keyboard := menu.GetRoleMenu(string(*user.Role))
		msg := tgbotapi.NewMessage(chatID, "Добро пожаловать! Выберите действие:")
		msg.ReplyMarkup = keyboard
		if _, err := tg.Send(bot, msg); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	// Все остальные команды требуют регистрации
	user, err := db.GetUserByTelegramID(ctx, database, chatID)
	registered := false
	if err == nil || user != nil && user.Role != nil {
		registered = true
	}

	if !registered {
		role := getUserFSMRole(chatID)
		if role != "" {
			auth.HandleFSMMessage(ctx, chatID, text, role, bot, database)
			return
		}

		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Вы не зарегистрированы. Пожалуйста, нажмите /start для начала.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	// 🔒 Глобальная защёлка: неактивным — ни одну команду
	if user != nil && !user.IsActive {
		rm := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
		// на случай, если у пользователя осталась старая клавиатура — уберём
		rm.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
		if _, err := tg.Send(bot, rm); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if handlers.GetAddScoreState(chatID) != nil {
		handlers.HandleAddScoreText(bot, msg)
		return
	}
	if handlers.GetRemoveScoreState(chatID) != nil {
		handlers.HandleRemoveText(ctx, bot, database, msg)
		return
	}
	if handlers.GetSetPeriodState(chatID) != nil {
		handlers.HandleSetPeriodInput(bot, msg)
		return
	}
	if handlers.GetAuctionState(chatID) != nil {
		handlers.HandleAuctionText(ctx, bot, database, msg)
		return
	}
	if handlers.GetExportState(chatID) != nil {
		handlers.HandleExportText(ctx, bot, database, msg)
		return
	}
	if handlers.GetAdminUsersState(chatID) != nil {
		handlers.HandleAdminUsersText(ctx, bot, database, msg)
		return
	}
	if handlers.GetCatalogState(chatID) != nil {
		handlers.HandleCatalogText(ctx, bot, database, msg)
		return
	}
	if auth.GetAddChildFSMState(chatID) != "" {
		auth.HandleAddChildText(ctx, bot, database, msg)
		return
	}

	switch text {
	case "/add_score", "➕ Начислить баллы":
		unlock := chatLimiter.lock(chatID)
		go func() {
			defer unlock()
			handlers.StartAddScoreFSM(ctx, bot, database, msg)
		}()
	case "/remove_score", "📉 Списать баллы":
		unlock := chatLimiter.lock(chatID)
		go func() {
			defer unlock()
			handlers.StartRemoveScoreFSM(ctx, bot, database, msg)
		}()
	case "/my_score", "📊 Мой рейтинг":
		go handlers.HandleMyScore(ctx, bot, database, msg)
	case "📜 История получения баллов":
		if user.Role != nil {
			switch *user.Role {
			case models.Student:
				handlers.StartStudentHistoryExcel(ctx, bot, database, msg)
			case models.Parent:
				handlers.StartParentHistoryExcel(ctx, bot, database, msg)
			default:
				if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Недоступно для вашей роли.")); err != nil {
					metrics.HandlerErrors.Inc()
				}
			}
		}
	case "➕ Добавить ребёнка":
		go auth.StartAddChild(bot, msg)
	case "📊 Рейтинг ребёнка":
		if *user.Role == models.Parent {
			go handlers.HandleParentRatingRequest(ctx, bot, database, chatID, user.ID)
		}
	case "/approvals", "📥 Заявки на баллы":
		if *user.Role == "admin" || *user.Role == "administration" {
			go handlers.ShowPendingScores(ctx, bot, database, chatID)
		}
	case "📥 Заявки на авторизацию":
		if db.IsAdminID(chatID) {
			go handlers.ShowPendingUsers(ctx, bot, database, chatID)
			go handlers.ShowPendingParentLinks(ctx, bot, database, chatID)
		}
	case "/periods", "📅 Периоды":
		if *user.Role == "admin" {
			go handlers.StartAdminPeriods(ctx, bot, database, msg)
		}
	case "/export", "📥 Экспорт отчёта":
		if *user.Role == "admin" || *user.Role == "administration" {
			unlock := chatLimiter.lock(chatID)
			go func() {
				defer unlock()
				handlers.StartExportFSM(ctx, bot, database, msg)
			}()
		}
	case "👥 Пользователи":
		if *user.Role == "admin" {
			go handlers.StartAdminUsersFSM(bot, msg)
		}
	case "/auction", "🎯 Аукцион":
		if *user.Role == "admin" || *user.Role == "administration" {
			go handlers.StartAuctionFSM(ctx, bot, database, msg)
		}
	case "🗂 Справочники":
		if *user.Role == "admin" {
			go handlers.StartCatalogFSM(ctx, bot, database, msg)
		}
	case "/backup", "💾 Бэкап БД":
		if user.Role != nil && (*user.Role == "admin") {
			unlock := chatLimiter.lock(chatID)
			go func() {
				defer unlock()
				handlers.HandleAdminBackup(ctx, bot, database, chatID)
			}()
		}
	case "♻️ Восстановить БД":
		if user.Role != nil && (*user.Role == "admin") {
			unlock := chatLimiter.lock(chatID)
			go func() {
				defer unlock()
				handlers.HandleAdminRestoreLatest(ctx, bot, database, chatID)
			}()
		}
	case "📥 Восстановить из файла":
		if user.Role != nil && (*user.Role == "admin") {
			unlock := chatLimiter.lock(chatID)
			go func() {
				defer unlock()
				handlers.HandleAdminRestoreStart(ctx, bot, database, chatID)
			}()
		}
	default:
		role := getUserFSMRole(chatID)
		if _, ok := handlers.PeriodsFSMActive(chatID); ok && user.Role != nil && (*user.Role == "admin") {
			handlers.HandleAdminPeriodsText(bot, msg)
			return
		}
		if role == "" {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		auth.HandleFSMMessage(ctx, chatID, text, role, bot, database)
	}
}

func HandleCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
		metrics.HandlerErrors.Inc()
	}
	data := cb.Data
	chatID := cb.Message.Chat.ID
	ctx = ctxutil.WithChatID(
		ctxutil.WithOp(ctx, "tg.callback:"+cb.Data),
		chatID,
	)

	// 🔒 Глобальная защёлка для inline-кнопок: неактивным всё режем
	// берём пользователя по Telegram ID отправителя колбэка.
	if cb.From != nil {
		if u, err := db.GetUserByTelegramID(ctx, database, cb.From.ID); err == nil && u != nil && !u.IsActive {
			// И даём явное сообщение в чат (на случай, если кнопка была из старого меню)
			msg := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
			// Уберём возможную «залипшую» клавиатуру
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			if _, err := tg.Send(bot, msg); err != nil {
				metrics.HandlerErrors.Inc()
			}
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

	if handlers.AdminRestoreFSMActive(chatID) && (data == "restore_cancel") {
		handlers.HandleAdminRestoreCallback(bot, cb)
		return
	}

	if strings.HasPrefix(data, "per_") || data == "per_confirm" {
		handlers.HandleSetPeriodCallback(ctx, bot, database, cb)
		return
	}

	if strings.HasPrefix(data, "link_confirm_") || strings.HasPrefix(data, "link_reject_") {
		handlers.HandleParentLinkApprovalCallback(ctx, cb, bot, database)
		return
	}

	if strings.HasPrefix(data, "confirm_") ||
		strings.HasPrefix(data, "reject_") {
		handlers.HandleAdminCallback(ctx, cb, database, bot, chatID)
		return
	}

	if strings.HasPrefix(data, "score_confirm_") ||
		strings.HasPrefix(data, "score_reject_") {
		handlers.HandleScoreApprovalCallback(ctx, cb, bot, database, chatID)
		return
	}
	// Student
	if strings.HasPrefix(data, "student_class_num_") ||
		strings.HasPrefix(data, "student_class_letter_") ||
		data == "student_back" || data == "student_cancel" {
		auth.HandleStudentCallback(ctx, cb, bot, database)
		return
	}
	if auth.GetAddChildFSMState(chatID) != "" {
		// Назад/Отмена add-child
		if data == "add_child_back" || data == "add_child_cancel" ||
			strings.HasPrefix(data, "parent_class_num_") ||
			strings.HasPrefix(data, "parent_class_letter_") {
			auth.HandleAddChildCallback(ctx, bot, database, cb)
			return
		}
	}
	// Parent
	if strings.HasPrefix(data, "parent_class_num_") ||
		strings.HasPrefix(data, "parent_class_letter_") ||
		data == "parent_back" || data == "parent_cancel" {
		auth.HandleParentCallback(ctx, bot, database, cb)
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
		handlers.HandleAddScoreCallback(ctx, bot, database, cb)
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
		handlers.HandleRemoveCallback(ctx, bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "export_type_") ||
		strings.HasPrefix(data, "export_period_") ||
		strings.HasPrefix(data, "export_mode_") ||
		strings.HasPrefix(data, "export_class_number_") ||
		strings.HasPrefix(data, "export_class_letter_") ||
		strings.HasPrefix(data, "export_select_student_") ||
		strings.HasPrefix(data, "export_schoolyear_") ||
		data == "export_students_done" ||
		data == "export_back" ||
		data == "export_cancel" {
		handlers.HandleExportCallback(ctx, bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "auction_mode_") ||
		strings.HasPrefix(data, "auction_class_number_") ||
		strings.HasPrefix(data, "auction_class_letter_") ||
		strings.HasPrefix(data, "auction_select_student_") ||
		data == "auction_students_done" ||
		data == "auction_back" ||
		data == "auction_cancel" {
		handlers.HandleAuctionCallback(ctx, bot, database, cb)
		return
	}
	if data == "add_another_child_yes" {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Введите ФИО следующего ребёнка:")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		msg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}} // мок-сообщение для FSM
		auth.StartAddChild(bot, msg)
		return
	}
	if data == "add_another_child_no" {
		msg := tgbotapi.NewMessage(chatID, "Вы вернулись в главное меню.")
		msg.ReplyMarkup = menu.GetRoleMenu("parent")
		if _, err := tg.Send(bot, msg); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if strings.HasPrefix(data, "show_rating_student_") {
		idStr := strings.TrimPrefix(data, "show_rating_student_")
		studentID, err := strconv.Atoi(idStr)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Ошибка: не удалось определить ученика.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		handlers.ShowStudentRating(ctx, bot, database, chatID, int64(studentID))
		return
	}
	if strings.HasPrefix(data, "hist_excel_student_") {
		handlers.HandleHistoryExcelCallback(ctx, bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "admusr_") {
		handlers.HandleAdminUsersCallback(ctx, bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "catalog_") ||
		data == "catalog_back" || data == "catalog_cancel" {
		handlers.HandleCatalogCallback(ctx, bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "exp_users_") {
		user, _ := db.GetUserByTelegramID(ctx, database, chatID)

		isAdmin := *user.Role == models.Admin || *user.Role == models.Administration
		switch data {
		case "exp_users_open":
			handlers.ClearExportState(chatID)
			// показать экран параметров экспорта
			handlers.StartExportUsers(bot, database, cb.Message, isAdmin)
		case "exp_users_toggle", "exp_users_gen", "exp_users_cancel", "exp_users_back":
			// обработать кнопки внутри экрана
			handlers.HandleExportUsersCallback(ctx, bot, database, cb, isAdmin)
		}
		return
	}
	// Периоды (админ): список и редактирование
	if data == "peradm_edit_end" || data == "peradm_edit_both" || data == "peradm_save" {
		handlers.HandleAdminPeriodsEditCallback(ctx, bot, database, cb)
		return
	}
	if strings.HasPrefix(data, "peradm_") {
		handlers.HandleAdminPeriodsCallback(ctx, bot, database, cb)
		return
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func getUserFSMRole(chatID int64) string {
	return db.UserFSMRole[chatID]
}
