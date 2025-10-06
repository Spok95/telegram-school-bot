package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type adminUsersState struct {
	Step           int
	Query          string
	SelectedUserID int64
	PendingRole    string
	ClassNumber    int64
	ClassLetter    string
	MessageID      int
}

var adminUsersStates = map[int64]*adminUsersState{}

func GetAdminUsersState(chatID int64) *adminUsersState { return adminUsersStates[chatID] }

// ─── ENTRY

func StartAdminUsersFSM(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	adminUsersStates[chatID] = &adminUsersState{Step: 1}
	edit := tgbotapi.NewMessage(chatID, "👥 Управление пользователями\nВведите имя или класс (например, 7А) для поиска:")
	edit.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		fsmutil.BackCancelRow("admusr_back_to_menu", "admusr_cancel"))
	sent, _ := tg.Send(bot, edit)
	adminUsersStates[chatID].MessageID = sent.MessageID
}

// ─── TEXT HANDLER

func HandleAdminUsersText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := adminUsersStates[chatID]
	if state == nil {
		return
	}

	switch state.Step {
	case 1:
		state.Query = strings.TrimSpace(msg.Text)
		users, err := db.FindUsersByQuery(database, state.Query, 50)
		if err != nil || len(users) == 0 {
			edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, "Ничего не найдено, попробуйте другой запрос.")
			mk := tgbotapi.NewInlineKeyboardMarkup(
				fsmutil.BackCancelRow("admusr_back_to_menu", "admusr_cancel"))
			edit.ReplyMarkup = &mk
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		text := fmt.Sprintf("Найдено %d пользователей. Выберите:", len(users))
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, u := range users {
			labelRole := "(нет роли)"
			if u.Role != nil {
				labelRole = string(*u.Role)
			}
			labelClass := ""
			if u.ClassNumber != nil && u.ClassLetter != nil {
				labelClass = fmt.Sprintf(" • %d%s", int(*u.ClassNumber), *u.ClassLetter)
			}
			btn := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s • %s%s", u.Name, labelRole, labelClass), fmt.Sprintf("admusr_pick_%d", u.ID))
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		}
		rows = append(rows, fsmutil.BackCancelRow("admusr_back_to_search", "admusr_cancel"))
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, text)
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		state.Step = 2
	case 3:
		num, let, ok := parseClass(msg.Text)
		if !ok {
			edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, "Неверный формат. Пример: 7А, 10Б, 11Г.\nВведите класс.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		state.ClassNumber, state.ClassLetter = num, let

		question := fmt.Sprintf("Сменить роль на Ученик (%d%s)?", state.ClassNumber, state.ClassLetter)
		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "admusr_apply_student"),
			),
			fsmutil.BackCancelRow("admusr_back_to_role", "admusr_cancel"),
		}
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, question)
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		state.Step = 4
		return
	}
}

// ─── CALLBACK HANDLER

func HandleAdminUsersCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	state := adminUsersStates[chatID]
	if state == nil {
		return
	}
	data := cb.Data

	// Отмена
	if data == "admusr_cancel" {
		fsmutil.DisableMarkup(bot, chatID, state.MessageID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, state.MessageID, "🚫 Отменено.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(adminUsersStates, chatID)
		return
	}

	// выбор пользователя из списка
	if strings.HasPrefix(data, "admusr_pick_") {
		var uid int64
		if _, err := fmt.Sscanf(data, "admusr_pick_%d", &uid); err != nil {
			return
		}
		state.SelectedUserID = uid

		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Ученик", "admusr_set_student"),
				tgbotapi.NewInlineKeyboardButtonData("Родитель", "admusr_set_parent"),
				tgbotapi.NewInlineKeyboardButtonData("Учитель", "admusr_set_teacher"),
				tgbotapi.NewInlineKeyboardButtonData("Администрация", "admusr_set_administration"),
				tgbotapi.NewInlineKeyboardButtonData("Админ", "admusr_set_admin"),
			),
		}

		// ряд управления активностью
		u, _ := db.GetUserByID(database, uid)
		var actBtn tgbotapi.InlineKeyboardButton
		if u.IsActive {
			actBtn = tgbotapi.NewInlineKeyboardButtonData("⛔️ Деактивировать", "admusr_deactivate")
		} else {
			actBtn = tgbotapi.NewInlineKeyboardButtonData("✅ Активировать", "admusr_activate")
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(actBtn))

		rows = append(rows, fsmutil.BackCancelRow("admusr_back_to_list", "admusr_cancel"))
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, "Выберите новую роль или измените активность:")
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	// ── управление активностью пользователя
	if data == "admusr_deactivate" {
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ок")); err != nil {
			metrics.HandlerErrors.Inc()
		}

		now := time.Now()
		if err := db.DeactivateUser(database, state.SelectedUserID, now); err != nil {
			log.Println("deactivate user error:", err)
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось деактивировать пользователя")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		// пересчитываем родителей, если это ученик (по связям; если не ученик — просто не будет строк)
		rows, err := database.Query(`SELECT parent_id FROM parents_students WHERE student_id = $1`, state.SelectedUserID)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var pid int64
				if scanErr := rows.Scan(&pid); scanErr == nil {
					_ = db.RefreshParentActiveFlag(database, pid)
				}
			}
		}
		// сообщим и перерисуем карточку
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Пользователь деактивирован")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		// триггерим заново отрисовку выбранного
		cb.Data = fmt.Sprintf("admusr_pick_%d", state.SelectedUserID)
		HandleAdminUsersCallback(bot, database, cb)
		return
	}
	if data == "admusr_activate" {
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Ок")); err != nil {
			metrics.HandlerErrors.Inc()
		}

		if err := db.ActivateUser(database, state.SelectedUserID); err != nil {
			log.Println("activate user error:", err)
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось активировать пользователя")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		// пересчитываем родителей, если это ученик
		rows, err := database.Query(`SELECT parent_id FROM parents_students WHERE student_id = $1`, state.SelectedUserID)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var pid int64
				if scanErr := rows.Scan(&pid); scanErr == nil {
					_ = db.RefreshParentActiveFlag(database, pid)
				}
			}
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Пользователь активирован")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		cb.Data = fmt.Sprintf("admusr_pick_%d", state.SelectedUserID)
		HandleAdminUsersCallback(bot, database, cb)
		return
	}

	if strings.HasPrefix(data, "admusr_set_") {
		role := strings.TrimPrefix(data, "admusr_set_")
		state.PendingRole = role

		// Для ученика сначала спросим класс
		if role == "student" {
			mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow("admusr_back_to_role", "admusr_cancel"))
			edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, "Введите класс в формате 7А:")
			edit.ReplyMarkup = &mk
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			state.Step = 3
			return
		}
		// Для остальных ролей сразу подтверждение
		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "admusr_apply_"+role),
			),
			fsmutil.BackCancelRow("admusr_back_to_role", "admusr_cancel"),
		}
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, fmt.Sprintf("Сменить роль на «%s»?", humanRole(role)))
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		state.Step = 4
		return
	}
	// подтверждение (общий случай) ИЛИ подтверждение для student
	if strings.HasPrefix(data, "admusr_apply_") || data == "admusr_apply_student" {
		role := strings.TrimPrefix(data, "admusr_apply_")
		if role == "" {
			role = state.PendingRole
		}
		admin, _ := db.GetUserByTelegramID(database, chatID)
		if admin == nil || admin.Role == nil || (*admin.Role != "admin") {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Нет прав.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		var err error
		if role == "student" || state.PendingRole == "student" {
			err = db.ChangeRoleToStudentWithAudit(database, state.SelectedUserID, state.ClassNumber, state.ClassLetter, admin.ID)
		} else {
			err = db.ChangeRoleWithCleanup(database, state.SelectedUserID, role, admin.ID)
		}
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Ошибка при смене роли: "+err.Error())); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		// уведомление пользователю
		target, _ := db.GetUserByID(database, state.SelectedUserID)
		txt := fmt.Sprintf("Ваша роль была изменена на «%s». Нажмите /start, чтобы обновить меню.", humanRole(role))
		if _, err := tg.Send(bot, tgbotapi.NewMessage(target.TelegramID, txt)); err != nil {
			metrics.HandlerErrors.Inc()
		}

		// ── РЕТРОСПЕКТИВА/АВТО-ДЕАКТИВАЦИЯ РОДИТЕЛЕЙ ─────────────────────────────
		// Если назначили роль родителя — пересчитать его активность
		if role == "parent" {
			if err := db.RefreshParentActiveFlag(database, state.SelectedUserID); err != nil {
				log.Println("refresh parent activity failed:", err)
			}
		}
		// Если назначили/изменили роль ученика — пересчитать активность всех его родителей
		if role == "student" {
			rows, err := database.Query(`SELECT parent_id FROM parents_students WHERE student_id = $1`, state.SelectedUserID)
			if err == nil {
				defer func() { _ = rows.Close() }()
				for rows.Next() {
					var pid int64
					if scanErr := rows.Scan(&pid); scanErr == nil {
						if err := db.RefreshParentActiveFlag(database, pid); err != nil {
							log.Println("refresh parent activity failed:", err)
						}
					}
				}
			} else {
				log.Println("list parents by student failed:", err)
			}
		}

		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, "✅ Роль обновлена")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(adminUsersStates, chatID)
		return
	}

	// ===== Назад
	if data == "admusr_back_to_role" {
		// вернуться к выбору роли
		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Ученик", "admusr_set_student"),
				tgbotapi.NewInlineKeyboardButtonData("Родитель", "admusr_set_parent"),
				tgbotapi.NewInlineKeyboardButtonData("Учитель", "admusr_set_teacher"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Администрация", "admusr_set_administration"),
				tgbotapi.NewInlineKeyboardButtonData("Админ", "admusr_set_admin"),
			),
			fsmutil.BackCancelRow("admusr_back_to_list", "admusr_cancel"),
		}
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, "Выберите новую роль:")
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		state.Step = 2
		return
	}
	if data == "admusr_back_to_list" {
		// восстановить список найденных по state.query
		users, _ := db.FindUsersByQuery(database, state.Query, 50)
		text := fmt.Sprintf("Найдено %d пользователей. Выберите:", len(users))
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, u := range users {
			labelRole := "(нет роли)"
			if u.Role != nil {
				labelRole = string(*u.Role)
			}
			labelClass := ""
			if u.ClassNumber != nil && u.ClassLetter != nil {
				labelClass = fmt.Sprintf(" • %d%s", int(*u.ClassNumber), *u.ClassLetter)
			}
			btn := tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%s • %s%s", u.Name, labelRole, labelClass),
				fmt.Sprintf("admusr_pick_%d", u.ID),
			)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		}
		rows = append(rows, fsmutil.BackCancelRow("admusr_back_to_search", "admusr_cancel"))
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID, text)
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		state.Step = 2
		return
	}
	// ← Назад к вводу запроса (из списка)
	if data == "admusr_back_to_search" {
		edit := tgbotapi.NewEditMessageText(chatID, state.MessageID,
			"👥 Управление пользователями\nВведите имя или класс (например, 7А) для поиска:")
		mk := tgbotapi.NewInlineKeyboardMarkup(fsmutil.BackCancelRow("admusr_back_to_menu", "admusr_cancel"))
		edit.ReplyMarkup = &mk
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		state.Step = 1
		return
	}

	// ← Назад в меню (как Отмена) — доступно с экрана ввода.
	if data == "admusr_back_to_menu" {
		fsmutil.DisableMarkup(bot, chatID, state.MessageID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, state.MessageID, "🚫 Отменено.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(adminUsersStates, chatID)
		return
	}
}

func humanRole(role string) string {
	switch role {
	case "student":
		return "Ученик"
	case "parent":
		return "Родитель"
	case "teacher":
		return "Учитель"
	case "administration":
		return "Администрация"
	case "admin":
		return "Админ"
	default:
		return role
	}
}

// parseClass: парсит ввод вроде "7А", "10Б", допускает латиницу (A→А и т.п.), приводит к верхнему регистру
func parseClass(s string) (int64, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, "", false
	}
	// найти цифровую часть в начале
	r := []rune(s)
	i := 0
	for i < len(r) && r[i] >= '0' && r[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(r) {
		return 0, "", false
	}
	numStr := string(r[:i])
	letter := strings.ToUpper(string(r[i:]))

	// латиница -> кириллица для похожих букв (первая буква)
	rep := map[rune]rune{
		'A': 'А', 'B': 'В', 'E': 'Е', 'K': 'К', 'M': 'М',
		'H': 'Н', 'O': 'О', 'P': 'Р', 'C': 'С', 'T': 'Т', 'X': 'Х',
	}
	lr := []rune(letter)
	if len(lr) != 1 {
		return 0, "", false
	}
	if rr, ok := rep[lr[0]]; ok {
		lr[0] = rr
	}
	letter = string(lr[0])

	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return num, letter, true
}
