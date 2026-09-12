package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/auth"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/export"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/observability"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	updGuard    = NewUpdateGuard()
	chatLimiter = NewChatLimiter()
)

func HandleMessage(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	// базовый контекст для обработки входящего сообщения
	ctx = ctxutil.WithChatID(
		ctxutil.WithOp(ctx, "tg.message"),
		chatID,
	)
	text := msg.Text
	// --- ранний отсев флуда/дублей
	if !updGuard.Allow(&tgbotapi.Update{Message: msg}) {
		return
	}
	db.EnsureAdmin(ctx, chatID, database, text, bot)

	// 🔁 Если активен FSM восстановления БД — делегируем туда любой апдейт (текст/документ)
	if handlers.AdminRestoreFSMActive(chatID) {
		handlers.HandleAdminRestoreMessage(ctx, bot, database, msg)
		return
	}

	// Обработка команды /start без проверки регистрации
	if text == "/start" {
		user, err := db.GetUserByTelegramID(ctx, database, chatID)
		if err != nil {
			if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось проверить учётную запись. Попробуйте позже.")); sendErr != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if user == nil || user.Role == nil {
			auth.CancelRecovery(chatID)
			db.ClearUserFSMRole(chatID)
			sendAuthEntryMenu(bot, chatID)
			return
		}
		if !user.Confirmed {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Ваша заявка на регистрацию ещё ожидает подтверждения администратора.")); err != nil {
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
	if err == nil && user != nil && user.Role != nil && user.Confirmed {
		registered = true
	}

	if !registered {
		if auth.GetRecoveryFSMState(chatID) != "" {
			auth.HandleRecoveryMessage(ctx, chatID, text, bot, database)
			return
		}
		if user != nil && user.Role != nil && !user.Confirmed {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Ваша заявка на регистрацию ещё ожидает подтверждения администратора.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
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
		handlers.HandleAddScoreText(ctx, bot, msg)
		return
	}
	if handlers.GetRemoveScoreState(chatID) != nil {
		handlers.HandleRemoveText(ctx, bot, database, msg)
		return
	}
	if handlers.GetSetPeriodState(chatID) != nil {
		handlers.HandleSetPeriodInput(ctx, bot, msg)
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
	if TryHandleTeacherLinkText(ctx, bot, database, msg) {
		return
	}
	if TryHandleTeacherAddSlots(ctx, bot, database, msg) {
		return
	}
	if TryHandleParentCommands(ctx, bot, database, msg) {
		return
	}
	// Учительский FSM /t_slots
	if TryHandleTeacherSlotsCommand(ctx, bot, database, msg) {
		return
	}
	if TryHandleTeacherSlotsText(ctx, bot, database, msg) {
		return
	}
	// Родитель: список слотов кнопками
	if TryHandleParentSlotsCommand(ctx, bot, database, msg) {
		return
	}
	// Учитель: список и управление слотами
	if TryHandleTeacherMySlots(ctx, bot, database, msg) {
		return
	}

	switch text {
	case "/add_score", "➕ Начислить баллы":
		unlock := chatLimiter.lock(chatID)
		defer unlock()
		handlers.StartAddScoreFSM(ctx, bot, database, msg)
	case "/remove_score", "📉 Списать баллы":
		unlock := chatLimiter.lock(chatID)
		defer unlock()
		handlers.StartRemoveScoreFSM(ctx, bot, database, msg)
	case "/my_score", "📊 Мой рейтинг":
		handlers.HandleMyScore(ctx, bot, database, msg)
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
		auth.StartAddChild(ctx, bot, msg)
	case "📊 Рейтинг ребёнка":
		if *user.Role == models.Parent {
			handlers.HandleParentRatingRequest(ctx, bot, database, chatID, user.ID)
		}
	case "/approvals", "📥 Заявки на баллы":
		if *user.Role == "admin" || *user.Role == "administration" {
			handlers.ShowPendingScores(ctx, bot, database, chatID)
		}
	case "📥 Заявки на авторизацию":
		if db.IsAdminID(chatID) {
			handlers.ShowPendingUsers(ctx, bot, database, chatID)
			handlers.ShowPendingAccountRecoveryRequests(ctx, bot, database, chatID)
			handlers.ShowPendingParentLinks(ctx, bot, database, chatID)
		}
	case "/periods", "📅 Периоды":
		if *user.Role == "admin" {
			handlers.StartAdminPeriods(ctx, bot, database, msg)
		}
	case "/export", "📥 Экспорт отчёта":
		if *user.Role == "admin" || *user.Role == "administration" {
			unlock := chatLimiter.lock(chatID)
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			go func(c context.Context) {
				defer unlock()
				defer cancel()
				handlers.StartExportFSM(c, bot, database, msg)
			}(bg)
		}
	case "👥 Пользователи":
		if *user.Role == "admin" {
			handlers.StartAdminUsersFSM(ctx, bot, msg)
		}
	case "/auction", "🎯 Аукцион":
		if *user.Role == "admin" || *user.Role == "administration" {
			handlers.StartAuctionFSM(ctx, bot, database, msg)
		}
	case "🗂 Справочники":
		if *user.Role == "admin" {
			handlers.StartCatalogFSM(ctx, bot, database, msg)
		}
	case "/backup", "💾 Бэкап БД":
		if user.Role != nil && (*user.Role == "admin") {
			unlock := chatLimiter.lock(chatID)
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			go func(c context.Context) {
				defer unlock()
				defer cancel()
				handlers.HandleAdminBackup(c, bot, database, chatID)
			}(bg)
		}
	case "♻️ Восстановить БД":
		if user.Role != nil && (*user.Role == "admin") {
			warn := "⚠️ВНИМАНИЕ!!!⚠️\n" +
				"Восстановление базы данных может привести к потере несохранённых данных.\n" +
				"Перед восстановлением рекомендуется сделать «💾 Бэкап БД».\n\n" +
				"Вы уверены, что хотите восстановить?"

			kb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ ДА", "restore_latest:yes"),
					tgbotapi.NewInlineKeyboardButtonData("Отменить", "restore_latest:no"),
				),
			)

			m := tgbotapi.NewMessage(chatID, warn)
			m.ReplyMarkup = kb
			_, _ = tg.Send(bot, m)
		}
	case "📥 Восстановить из файла":
		if user.Role != nil && (*user.Role == "admin") {
			unlock := chatLimiter.lock(chatID)
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			go func(c context.Context) {
				defer unlock()
				defer cancel()
				handlers.HandleAdminRestoreStart(c, bot, database, chatID)
			}(bg)
		}
	case "/consult_help":
		reply(bot, chatID, "Консультации:\n"+
			"• Учитель: /t_slots — пошаговое создание слотов на 4 недели.\n"+
			"• Учитель: /t_addslots <день> <HH:MM-HH:MM> <шаг-мин> <class_id>\n"+
			"• Родитель: /p_slots <teacher_id> <YYYY-MM-DD> — свободные слоты кнопками.\n"+
			"• Родитель: /p_free <teacher_id> <YYYY-MM-DD> — свободные слоты списком.\n"+
			"• Родитель: /p_book <slot_id> — бронирование по ID.")
		return
	case "🗓 Создать слоты":
		// запускаем мастер
		if user.Role != nil && *user.Role == models.Teacher {
			// эмулируем /t_slots
			msg := *msg
			msg.Text = "/t_slots"
			if TryHandleTeacherSlotsCommand(ctx, bot, database, &msg) {
				return
			}
		}
	case "📋 Мои слоты":
		if user.Role != nil && *user.Role == models.Teacher {
			// эмулируем /t_myslots
			msg := *msg
			msg.Text = "/t_myslots"
			if TryHandleTeacherMySlots(ctx, bot, database, &msg) {
				return
			}
		}
	case "📅 Записаться на консультацию":
		if user.Role != nil && *user.Role == models.Parent {
			// стартуем parent-флоу выбора учителя/даты
			StartParentConsultFlow(ctx, bot, database, msg)
			return
		}
	case "📋 Мои записи":
		if user.Role != nil && *user.Role == models.Parent {
			from := time.Now().Add(-time.Hour) // показываем и «почти сейчас»
			items, err := db.ListParentBookings(ctx, database, user.ID, from, 50)
			if err != nil {
				if _, e := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось получить список записей.")); e != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}

			if len(items) == 0 {
				// Пусто — всё равно отдадим кнопку «Обновить список», которая вызывает p_my_consults
				kb := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить список", "p_my_consults"),
					),
				)
				m := tgbotapi.NewMessage(chatID, "Записей не найдено.")
				m.ReplyMarkup = kb
				if _, e := tg.Send(bot, m); e != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}

			// Есть записи — соберём кнопки отмены и «Обновить список»
			var rows [][]tgbotapi.InlineKeyboardButton
			for _, it := range items {
				fmtLabel := "оффлайн"
				if it.ConsultFormat == "online" {
					fmtLabel = "онлайн"
				}
				label := fmt.Sprintf("%s • %s — %s", it.StartAt.Format("02.01 15:04"), fmtLabel, it.Teacher)
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("❌ Отменить: "+label, fmt.Sprintf("p_cancel:%d", it.SlotID)),
				))
			}
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить список", "p_my_consults"),
			))
			kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

			m := tgbotapi.NewMessage(chatID, "Ваши записи на консультации:")
			m.ReplyMarkup = kb
			if _, e := tg.Send(bot, m); e != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

	case "📘 Расписание", "📘 Мои консультации", "Расписание", "Мои консультации":
		if user.Role != nil && *user.Role == models.Teacher {
			loc := time.Local
			now := time.Now().In(loc)
			from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			to := from.AddDate(0, 0, 14) // 14 дней вперёд

			go func() {
				ctxExp, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()

				path, err := export.ConsultationsExcelExport(ctxExp, database, user.ID, from, to, loc)
				if err != nil {
					log.Printf("[report_consultations] chat=%d user=%d err=%v", chatID, user.ID, err)
					observability.CaptureErr(err)
					if _, e := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось сформировать отчёт.")); e != nil {
						metrics.HandlerErrors.Inc()
					}
					return
				}

				doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(path))
				doc.Caption = "📘 Мои консультации"
				if _, e := tg.Send(bot, doc); e != nil {
					metrics.HandlerErrors.Inc()
				}
			}()
		}
		return

	case "📈 Отчёт консультаций", "/consult_report":
		if user.Role != nil && (*user.Role == models.Admin || *user.Role == models.Administration) {
			loc := time.Local
			now := time.Now().In(loc)
			from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			to := from.AddDate(0, 0, 14)
			go func() {
				ctxExp, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				path, err := export.ConsultationsExcelExportAdmin(ctxExp, database, from, to, loc)
				if err != nil {
					log.Printf("[report_consultations] chat=%d user=%d err=%v", chatID, user.ID, err)
					observability.CaptureErr(err)
					if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось сформировать отчёт.")); err != nil {
						metrics.HandlerErrors.Inc()
					}
					return
				}
				doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(path))
				doc.Caption = "📘 Расписание консультаций (админ)"
				if _, err := tg.Send(bot, doc); err != nil {
					metrics.HandlerErrors.Inc()
				}
			}()
			return
		}

	default:
		role := getUserFSMRole(chatID)
		if _, ok := handlers.PeriodsFSMActive(chatID); ok && user.Role != nil && (*user.Role == "admin") {
			handlers.HandleAdminPeriodsText(ctx, bot, msg)
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
	if cb == nil || cb.Message == nil || cb.From == nil {
		return
	}

	data := cb.Data
	chatID := cb.Message.Chat.ID

	// --- ранний отсев флуда/дублей
	if !updGuard.Allow(&tgbotapi.Update{CallbackQuery: cb}) {
		// мгновенный ACK, чтобы не висели «часики» даже если дропнули
		_, _ = tg.Request(bot, tgbotapi.NewCallback(cb.ID, ""))
		return
	}

	// обычный быстрый ACK перед основной логикой
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
		metrics.HandlerErrors.Inc()
	}

	ctx = ctxutil.WithChatID(
		ctxutil.WithOp(ctx, "tg.callback:"+cb.Data),
		chatID,
	)

	// Публичными остаются только кнопки первичной регистрации и восстановления доступа.
	// Они разрешены только Telegram ID, который ещё не привязан к подтверждённой учётной записи.
	// Все остальные inline-действия требуют существующего, подтверждённого и активного пользователя.
	actor, actorErr := db.GetUserByTelegramID(ctx, database, cb.From.ID)
	publicAuth := isPublicAuthCallback(data, chatID)
	if publicAuth {
		if actorErr != nil {
			if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Не удалось проверить учётную запись. Попробуйте позже.")); sendErr != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if actor != nil && actor.Role != nil {
			text := "⚠️ Эта кнопка больше недействительна. Нажмите /start."
			if !actor.Confirmed {
				text = "⏳ Ваша заявка на регистрацию ещё ожидает подтверждения администратора."
			}
			if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(chatID, text)); sendErr != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
	} else {
		if actorErr != nil || actor == nil || actor.Role == nil || !actor.Confirmed {
			if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Доступ к этой кнопке больше недействителен. Нажмите /start.")); sendErr != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if !actor.IsActive {
			msg := tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			if _, sendErr := tg.Send(bot, msg); sendErr != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
	}

	if isSystemAdminOnlyCallback(data) && !db.IsAdminID(cb.From.ID) {
		if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(chatID, "⛔ Недостаточно прав для выполнения этого действия.")); sendErr != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	log.Printf("CB from %d: %s (msgID=%d)\n", cb.From.ID, cb.Data, cb.Message.MessageID)

	if data == "auth_new" {
		auth.CancelRecovery(chatID)
		db.ClearUserFSMRole(chatID)
		sendRegistrationRoleMenu(bot, chatID, cb.Message.MessageID)
		return
	}
	if data == "auth_existing" {
		db.ClearUserFSMRole(chatID)
		auth.StartAccountRecovery(ctx, chatID, bot)
		return
	}
	if strings.HasPrefix(data, "recovery_role_") ||
		strings.HasPrefix(data, "recovery_class_num_") ||
		strings.HasPrefix(data, "recovery_class_letter_") ||
		data == "recovery_back" || data == "recovery_cancel" {
		auth.HandleRecoveryCallback(ctx, bot, database, cb)
		return
	}

	if strings.HasPrefix(data, "reg_") {
		role := strings.TrimPrefix(data, "reg_")
		db.SetUserFSMRole(chatID, role)
		if role == "parent" {
			auth.StartParentRegistration(ctx, chatID, cb.From, bot)
		} else {
			auth.StartRegistration(ctx, chatID, role, bot)
		}
		return
	}

	if strings.HasPrefix(data, "recovery_approve_") || strings.HasPrefix(data, "recovery_reject_") {
		handlers.HandleAccountRecoveryAdminCallback(ctx, bot, database, cb)
		return
	}

	// Учитель: управление слотами (удалить/отменить)
	if TryHandleTeacherManageCallback(ctx, bot, database, cb) {
		return
	}
	// Учительский FSM /t_slots (кнопки)
	if TryHandleTeacherSlotsCallback(ctx, bot, database, cb) {
		return
	}
	// Родитель: кнопка бронирования
	if TryHandleParentBookCallback(ctx, bot, database, cb) {
		return
	}
	if TryHandleParentFlowCallbacks(ctx, bot, database, cb) {
		return
	}
	// Родитель: «мои записи» (список) и «отмена записи»
	if TryHandleParentMyConsultsCallback(ctx, bot, database, cb) {
		return
	}
	if TryHandleParentCancelCallback(ctx, bot, database, cb) {
		return
	}

	if handlers.AdminRestoreFSMActive(chatID) && (data == "restore_cancel") {
		handlers.HandleAdminRestoreCallback(ctx, bot, database, cb)
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
		data == "add_comment" ||
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
		auth.StartAddChild(ctx, bot, msg)
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
			handlers.StartExportUsers(ctx, bot, database, cb.Message, isAdmin)
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
	// подтверждение восстановления «последнего» бэкапа
	if data == "restore_latest:yes" {
		// уберём инлайн-клавиатуру у предупреждения
		_, _ = tg.Send(bot, tgbotapi.NewEditMessageReplyMarkup(
			chatID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))

		unlock := chatLimiter.lock(chatID)
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		go func(c context.Context) {
			defer unlock()
			defer cancel()
			handlers.HandleAdminRestoreLatest(c, bot, database, chatID)
		}(bg)
		return
	}
	if data == "restore_latest:no" {
		_, _ = tg.Send(bot, tgbotapi.NewEditMessageReplyMarkup(
			chatID, cb.Message.MessageID, tgbotapi.InlineKeyboardMarkup{}))
		_, _ = tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Восстановление отменено."))
		return
	}

	// починка отмены при «📥 Восстановить из файла»
	if data == "restore_cancel" {
		handlers.HandleAdminRestoreCallback(ctx, bot, database, cb) // в файле admin_restore.go это уже реализовано
		return
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Неизвестная команда. Используйте /start")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func sendAuthEntryMenu(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Добро пожаловать! Выберите действие:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🆕 Новый пользователь", "auth_new"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔑 Войти в существующую учётную запись", "auth_existing"),
		),
	)
	if _, err := tg.Send(bot, msg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func sendRegistrationRoleMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "Выберите роль для регистрации:")
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
	edit.ReplyMarkup = &roles
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func isPublicAuthCallback(data string, chatID int64) bool {
	if data == "auth_new" || data == "auth_existing" || data == "recovery_back" || data == "recovery_cancel" {
		return true
	}
	alwaysPublicPrefixes := []string{
		"reg_",
		"recovery_role_",
		"recovery_class_num_",
		"recovery_class_letter_",
	}
	for _, prefix := range alwaysPublicPrefixes {
		if strings.HasPrefix(data, prefix) {
			return true
		}
	}

	role := getUserFSMRole(chatID)
	if role == string(models.Student) {
		return strings.HasPrefix(data, "student_class_num_") ||
			strings.HasPrefix(data, "student_class_letter_") ||
			data == "student_back" || data == "student_cancel"
	}
	if role == string(models.Parent) && auth.GetAddChildFSMState(chatID) == "" {
		return strings.HasPrefix(data, "parent_class_num_") ||
			strings.HasPrefix(data, "parent_class_letter_") ||
			data == "parent_back" || data == "parent_cancel"
	}
	return false
}

func isSystemAdminOnlyCallback(data string) bool {
	if data == "restore_cancel" || strings.HasPrefix(data, "restore_latest:") {
		return true
	}
	prefixes := []string{
		"confirm_",
		"reject_",
		"link_confirm_",
		"link_reject_",
		"admusr_",
		"peradm_",
		"per_",
		"recovery_approve_",
		"recovery_reject_",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(data, prefix) {
			return true
		}
	}
	return false
}

func getUserFSMRole(chatID int64) string {
	return db.UserFSMRole[chatID]
}
