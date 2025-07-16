package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strings"
	"sync"
	"time"
)

type AuthState string

const (
	AuthStateStart       AuthState = "start"
	AuthStateFIO         AuthState = "fio"
	AuthStateRole        AuthState = "role"
	AuthStateClass       AuthState = "class"
	AuthStateChild       AuthState = "child"
	AuthStateWaitConfirm AuthState = "wait_confirm"
	AuthStateDone        AuthState = "done"
)

type AuthSession struct {
	TelegramID  int64
	State       AuthState
	FIO         string
	Role        string
	Class       string
	ChildFIO    string
	RequestedAt time.Time
}

var authFSM sync.Map

// Запуск FSM
func StartFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	session := &AuthSession{
		TelegramID:  msg.From.ID,
		State:       AuthStateFIO,
		RequestedAt: time.Now(),
	}
	authFSM.Store(msg.From.ID, session)
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Введите ваши ФИО (например: Иванов Иван Иванович):"))
}

// FSM-шаг: ФИО

func HandleFIO(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	val, ok := authFSM.Load(msg.From.ID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка авторизации. Попробуйте /start заново."))
		return
	}
	session := val.(*AuthSession)
	session.FIO = msg.Text
	session.State = AuthStateRole
	authFSM.Store(msg.From.ID, session)
	// Клавиатура ролей
	buttons := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Ученик", "role_student"),
			tgbotapi.NewInlineKeyboardButtonData("Родитель", "role_parent"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Учитель", "role_teacher"),
		),
	)
	msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Выберите вашу роль:")
	msgOut.ReplyMarkup = buttons
	bot.Send(msgOut)
}

// FSM-шаг: ROLE (обработка inline-кнопок)
func HandleRoleInline(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	session, ok := AuthFSMGetSession(cb.From.ID)
	if !ok {
		return
	}
	data := cb.Data
	var role string
	switch data {
	case "role_student":
		role = "student"
	case "role_parent":
		role = "parent"
	case "role_teacher":
		role = "teacher"
	default:
		return
	}
	session.Role = role
	switch role {
	case "student":
		session.State = AuthStateClass
		authFSM.Store(cb.From.ID, session)
		bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Введите номер вашего класса (например: 7А):"))
	case "parent":
		session.State = AuthStateChild
		authFSM.Store(cb.From.ID, session)
		bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Введите класс и ФИО вашего ребёнка через пробел (например: 7Б Иванов Иван Иванович):"))
	default:
		session.State = AuthStateWaitConfirm
		authFSM.Store(cb.From.ID, session)
		bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Ваша заявка отправлена администратору. Ожидайте подтверждения!"))
	}
	bot.Request(tgbotapi.NewCallback(cb.ID, "Выбрано: "+role))
}

// FSM-шаг: CLASS и CHILD — аналогично, через обычные сообщения
func HandleClass(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	val, ok := authFSM.Load(msg.From.ID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка авторизации. Попробуйте /start заново."))
		return
	}
	session := val.(*AuthSession)
	session.Class = msg.Text
	session.State = AuthStateWaitConfirm
	authFSM.Store(msg.From.ID, session)

	err := db.UpdateUserPendingApplication(database,
		session.TelegramID,
		session.Role,
		session.FIO,
		session.Class,
		"",
	)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при сохранении заявки, попробуйте позже."))
		return
	}
	NotifyAdminOfPendingRole(bot, database, session.TelegramID, session.FIO, session.Role, session.Class, "")
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ваша заявка отправлена администратору. Ожидайте подтверждения!"))
}

func HandleChild(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	val, ok := authFSM.Load(msg.From.ID)
	if !ok {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка авторизации. Попробуйте /start заново."))
		return
	}
	session := val.(*AuthSession)
	parts := strings.SplitN(msg.Text, " ", 2)
	if len(parts) != 2 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Введите класс и ФИО ребёнка через пробел (например: 7Б Иванов Иван)"))
		return
	}
	session.Class = parts[0]
	session.ChildFIO = parts[1]
	session.State = AuthStateWaitConfirm
	authFSM.Store(msg.From.ID, session)
	// Сохраняем заявку, уведомляем пользователя
	err := db.UpdateUserPendingApplication(database,
		session.TelegramID,
		session.Role,
		session.FIO,
		session.Class,
		session.ChildFIO,
	)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при сохранении заявки, попробуйте позже."))
		return
	}
	NotifyAdminOfPendingRole(bot, database, session.TelegramID, session.FIO, session.Role, session.Class, session.ChildFIO)
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ваша заявка отправлена администратору. Ожидайте подтверждения!"))
}

// Функция, чтобы получить текущую сессию по ID
func AuthFSMGetSession(userID int64) (*AuthSession, bool) {
	val, ok := authFSM.Load(userID)
	if !ok {
		return nil, false
	}
	return val.(*AuthSession), true
}

func AuthFSMDeleteSession(userID int64) {
	authFSM.Delete(userID)
}

func NotifyAdminOfPendingRole(bot *tgbotapi.BotAPI, database *sql.DB, telegramID int64, fio, role, class, childFio string) {
	rows, err := database.Query(`SELECT telegram_id FROM users WHERE role = 'admin' AND is_active = 1`)
	if err != nil {
		log.Println("Ошибка при поиске администраторов:", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var adminID int64
		if err := rows.Scan(&adminID); err != nil {
			log.Println("Ошибка при чтении adminID:", err)
			continue
		}
		text := fmt.Sprintf("🔔 Новая заявка на добавление\nФИО: %s\nРоль: %s\nКласс: %s\nРебёнок: %s", fio, role, class, childFio)
		msg := tgbotapi.NewMessage(adminID, text)
		msg.ParseMode = "Markdown"
		buttons := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("approve_%d_%s", telegramID, role)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", telegramID)),
			),
		)
		msg.ReplyMarkup = buttons
		bot.Send(msg)
	}
}
