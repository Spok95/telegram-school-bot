package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"os"
)

func HandleStart(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	telegramID := msg.From.ID
	fullName := msg.From.FirstName + " " + msg.From.LastName
	roleText := "не назначена"
	text := ""
	// Проверяем, есть ли пользователь в базе
	user, err := db.GetUserByTelegramID(database, telegramID)
	if err != nil {
		// Новый пользователь — создаём в базе
		_, err := database.Exec(`
INSERT INTO users (telegram_id, name, is_active)
VALUES (?, ?, ?)`,
			telegramID, fullName, true)
		if err != nil {
			log.Println("Ошибка при создании пользователя:", err)
			sendText(bot, msg.Chat.ID, "Произошла ошибка при регистрации. Попробуйте позже.")
			return
		}

		// Установим роль "admin", если Telegram ID совпадает
		adminID := os.Getenv("ADMIN_ID")
		if adminID != "" && adminID == fmt.Sprint(telegramID) {
			_, err = database.Exec(`UPDATE users SET role = ?, is_active = 1 WHERE telegram_id = ?`, "admin", telegramID)
			if err != nil {
				log.Println("❌ Не удалось назначить роль администратора:", err)
			}
		}
	}

	// Пользователь найден — используем user.Role и user.IsActive
	if !user.IsActive {
		sendText(bot, msg.Chat.ID, "🚫 Ваш доступ временно ограничен. Обратитесь к администрации.")
		return
	}

	if user.Role != nil {
		roleText = string(*user.Role)
	}

	text = fmt.Sprintf("👋 Привет, %s!\nВаша роль: %s", user.Name, roleText)

	// Меню по ролям
	var keyboard tgbotapi.ReplyKeyboardMarkup

	switch roleText {
	case "student":
		keyboard = studentMenu()
	case "teacher":
		keyboard = teacherMenu()
	case "admin":
		keyboard = adminMenu()
	case "parent":
		keyboard = parentMenu()
	default:
		msgOut := tgbotapi.NewMessage(msg.Chat.ID, text)
		msgOut.ReplyMarkup = tgbotapi.ReplyKeyboardRemove{RemoveKeyboard: true}
		bot.Send(msgOut)
		return
	}

	msgOut := tgbotapi.NewMessage(msg.Chat.ID, text)
	msgOut.ReplyMarkup = keyboard
	bot.Send(msgOut)
}

func sendText(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	bot.Send(msg)
}

func studentMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Мой рейтинг"),
		),
	)
}

func teacherMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Начислить баллы"),
			tgbotapi.NewKeyboardButton("📉 Списать баллы"),
		),
	)
}

func adminMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Подтвердить списания"),
			tgbotapi.NewKeyboardButton("📊 Отчёты"),
		),
	)
}

func parentMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Рейтинг ребёнка"),
		),
	)
}
