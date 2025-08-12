package handlers

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type adminUsersState struct {
	Step           int
	Query          string
	SelectedUserID int64
	PendingRole    string
	ClsNum         int64
	ClsLet         string
}

var adminUsersStates = map[int64]*adminUsersState{}

func GetAdminUsersState(chatID int64) *adminUsersState { return adminUsersStates[chatID] }

// ─── ENTRY

func StartAdminUsersFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	adminUsersStates[chatID] = &adminUsersState{Step: 1}
	bot.Send(tgbotapi.NewMessage(chatID, "👥 Управление пользователями\nВведите имя или класс (например, 7А) для поиска:"))
}

// ─── TEXT HANDLER

func HandleAdminUsersText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	st := adminUsersStates[chatID]
	if st == nil {
		return
	}

	switch st.Step {
	case 1:
		st.Query = strings.TrimSpace(msg.Text)
		users, err := db.FindUsersByQuery(database, st.Query, 50)
		if err != nil || len(users) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "Ничего не найдено, попробуйте другой запрос."))
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
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = mk
		bot.Send(msg)
		st.Step = 2
	case 3:
		// ожидаем ввод класса для роли "student"
		num, let, ok := parseClass(msg.Text)
		if !ok {
			bot.Send(tgbotapi.NewMessage(chatID, "Неверный формат. Пример: 7А, 10Б, 11Г."))
			return
		}
		st.ClsNum, st.ClsLet = num, let

		question := fmt.Sprintf("Сменить роль на Ученик (%d%s)?", num, let)
		mk := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "admusr_apply_student"),
				tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "admusr_back"),
			),
		)
		out := tgbotapi.NewMessage(chatID, question)
		out.ReplyMarkup = mk
		bot.Send(out)
		st.Step = 4
	}
}

// ─── CALLBACK HANDLER

func HandleAdminUsersCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	st := adminUsersStates[chatID]
	if st == nil {
		return
	}
	data := cb.Data

	// выбор пользователя из списка
	if strings.HasPrefix(data, "admusr_pick_") {
		var uid int64
		fmt.Sscanf(data, "admusr_pick_%d", &uid)
		st.SelectedUserID = uid

		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Ученик", "admusr_set_student"),
				tgbotapi.NewInlineKeyboardButtonData("Родитель", "admusr_set_parent"),
				tgbotapi.NewInlineKeyboardButtonData("Учитель", "admusr_set_teacher"),
				tgbotapi.NewInlineKeyboardButtonData("Администрация", "admusr_set_administration"),
				tgbotapi.NewInlineKeyboardButtonData("Админ", "admusr_set_admin"),
			),
		}
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		cfg := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Выберите новую роль:")
		cfg.ReplyMarkup = &mk
		bot.Send(cfg)
		return
	}

	if strings.HasPrefix(data, "admusr_set_") {
		role := strings.TrimPrefix(data, "admusr_set_")
		st.PendingRole = role

		// Для ученика сначала спросим класс
		if role == "student" {
			cfg := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Введите класс в формате 7А:")
			bot.Send(cfg)
			st.Step = 3
			return
		}
		// Для остальных ролей сразу подтверждение
		mk := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("admusr_apply_%s", role)),
				tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "admusr_back"),
			),
		)

		question := fmt.Sprintf("Сменить роль на %s?", humanRole(role))
		cfg := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, question)
		cfg.ReplyMarkup = &mk
		bot.Send(cfg)
		return
	}
	// подтверждение (общий случай) ИЛИ подтверждение для student
	if strings.HasPrefix(data, "admusr_apply_") || data == "admusr_apply_student" {
		role := strings.TrimPrefix(data, "admusr_apply_")
		if role == "" {
			role = st.PendingRole
		}
		admin, _ := db.GetUserByTelegramID(database, chatID)
		if admin == nil || admin.Role == nil || (*admin.Role != "admin") {
			bot.Send(tgbotapi.NewMessage(chatID, "Нет прав."))
			return
		}

		var err error
		if role == "student" || st.PendingRole == "student" {
			err = db.ChangeRoleToStudentWithAudit(database, st.SelectedUserID, st.ClsNum, st.ClsLet, admin.ID)
		} else {
			err = db.ChangeRoleWithCleanup(database, st.SelectedUserID, role, admin.ID)
		}
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка при смене роли: "+err.Error()))
			return
		}

		// уведомление пользователю
		target, _ := db.GetUserByID(database, st.SelectedUserID)
		txt := fmt.Sprintf("Ваша роль была изменена на «%s». Нажмите /start, чтобы обновить меню.", humanRole(role))
		bot.Send(tgbotapi.NewMessage(target.TelegramID, txt))

		doneCfg := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "✅ Роль обновлена")
		bot.Send(doneCfg)
		delete(adminUsersStates, chatID)
		return
	}

	// назад к выбору ролей
	if data == "admusr_back" {
		mk := rolesMarkup()
		cfg := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Выберите новую роль:")
		cfg.ReplyMarkup = &mk
		bot.Send(cfg)
		st.Step = 2
		return
	}
}

// ─── HELPERS

func rolesMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Ученик", "admusr_set_student"),
			tgbotapi.NewInlineKeyboardButtonData("Родитель", "admusr_set_parent"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Учитель", "admusr_set_teacher"),
			tgbotapi.NewInlineKeyboardButtonData("Администрация", "admusr_set_administration"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Админ", "admusr_set_admin"),
		),
	)
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
