package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"os"
	"strconv"
	"strings"
)

func ShowPendingUsers(database *sql.DB, bot *tgbotapi.BotAPI) {
	adminIDStr := os.Getenv("ADMIN_ID")
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		log.Println("Ошибка при чтении ADMIN_ID из .env:", err)
		return
	}

	rows, err := database.Query(`
		SELECT id, name, role, telegram_id FROM users WHERE confirmed = 0 AND role != 'admin'
	`)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(adminID, "Ошибка при получении заявок."))
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var name, role string
		var tgID int64

		rows.Scan(&id, &name, &role, &tgID)

		msg := fmt.Sprintf("Заявка:\n👤 %s\n🧩 Роль: %s\nTelegramID: %d", name, role, tgID)

		btnYes := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", id))
		brnNo := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", id))
		markup := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btnYes, brnNo))

		message := tgbotapi.NewMessage(adminID, msg)
		message.ReplyMarkup = markup
		bot.Send(message)
	}
}

func HandleAdminCallback(callback *tgbotapi.CallbackQuery, database *sql.DB, bot *tgbotapi.BotAPI, adminID int64) {
	data := callback.Data
	if strings.HasPrefix(data, "confirm_") {
		idStr := strings.TrimPrefix(data, "confirm_")
		err := ConfirmUser(database, bot, idStr, adminID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(adminID, "Ошибка подтверждения заявки."))
			return
		}
		bot.Send(tgbotapi.NewMessage(adminID, "✅ Пользователь подтверждён."))
	} else if strings.HasPrefix(data, "reject_") {
		idStr := strings.TrimPrefix(data, "reject_")
		err := RejectUser(database, bot, idStr, adminID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(adminID, "Ошибка отклонения заявки."))
			return
		}
		bot.Send(tgbotapi.NewMessage(adminID, "❌ Заявка отклонена."))
	}
	callbackConfig := tgbotapi.CallbackConfig{
		CallbackQueryID: callback.ID,
		Text:            "Обработано",
		ShowAlert:       false,
	}
	bot.Request(callbackConfig)
}

func ConfirmUser(database *sql.DB, bot *tgbotapi.BotAPI, name string, adminID int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var telegramID int64
	err = database.QueryRow(`SELECT telegram_id FROM users WHERE id = ?`, name).Scan(&telegramID)
	if err != nil {
		return err
	}

	// Получаем текущую роль (до подтверждения)
	var role string
	err = tx.QueryRow(`SELECT role FROM users WHERE id = ? AND confirmed = 0`, name).Scan(&role)
	if err != nil {
		// либо уже подтверждён, либо не найден
		return fmt.Errorf("заявка не найдена или уже обработана")
	}

	// Подтверждаем, только если ещё не подтверждён
	res, err := tx.Exec(`UPDATE users SET confirmed = 1 WHERE id = ? AND confirmed = 0`, name)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("заявка уже подтверждена другим админом")
	}

	msg := tgbotapi.NewMessage(telegramID, "✅ Ваша заявка подтверждена. Добро пожаловать!")
	msg.ReplyMarkup = menu.GetRoleMenu(role)
	bot.Send(msg)

	// Фиксируем в истории
	_, err = tx.Exec(`
		INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, name, "unconfirmed", role, adminID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func RejectUser(database *sql.DB, bot *tgbotapi.BotAPI, name string, adminID int64) error {
	var telegramID int64
	err := database.QueryRow(`SELECT telegram_id FROM users WHERE id = ?`, name).Scan(&telegramID)
	if err != nil {
		return err
	}

	_, err = database.Exec(`DELETE FROM users WHERE id = ?`, name)
	if err != nil {
		return err
	}

	bot.Send(tgbotapi.NewMessage(telegramID, "❌ Ваша заявка отклонена. Попробуйте позже или обратитесь к администратору."))
	return nil
}
