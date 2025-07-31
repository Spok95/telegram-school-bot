package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
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

		var msg string

		if role == "student" {
			var classNumber, classLetter sql.NullString
			err := database.QueryRow(`SELECT class_number, class_letter FROM users WHERE id = ?`, id).Scan(&classNumber, &classLetter)
			if err != nil {
				log.Println("Ошибка при получении класса ученика:", err)
				continue
			}
			msg = fmt.Sprintf(
				"Заявка:\n👤 %s\n🏫 Класс: %s%s\n🧩 Роль: %s\nTelegramID: %d",
				name,
				classNumber.String, classLetter.String,
				role, tgID,
			)
		} else if role == "parent" {
			var studentName, studentClassNumber, studentClassLetter sql.NullString

			// получаем имя родителя (Telegram username или имя из Telegram профиля, если есть)
			err := database.QueryRow(`
			SELECT u.name, u.class_number, u.class_letter
			FROM users u
			JOIN parents_students ps ON ps.student_id = u.id
			WHERE ps.parent_id = ?
		`, id).Scan(&studentName, &studentClassNumber, &studentClassLetter)
			if err != nil {
				log.Println("Ошибка при получении информации о ребёнке:", err)
				continue
			}

			msg = fmt.Sprintf(
				"Заявка:\n👤 Родитель: %s\n👤 Ребёнок: %s\n🏫 Класс: %s%s\n🧩 Роль: %s\nTelegramID: %d",
				name, studentName.String, studentClassNumber.String, studentClassLetter.String,
				role, tgID,
			)
		} else {
			// fallback
			msg = fmt.Sprintf("Заявка:\n👤 %s\n🧩 Роль: %s\nTelegramID: %d", name, role, tgID)
		}

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
	messageID := callback.Message.MessageID
	chatID := callback.Message.Chat.ID
	adminUsername := callback.From.UserName

	if strings.HasPrefix(data, "confirm_") {
		idStr := strings.TrimPrefix(data, "confirm_")

		err := ConfirmUser(database, bot, idStr, adminID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(adminID, "❌ Ошибка подтверждения заявки."))
			return
		}

		newText := fmt.Sprintf("✅ Заявка подтверждена.\nПодтвердил: @%s", adminUsername)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, newText)
		bot.Send(edit)
	} else if strings.HasPrefix(data, "reject_") {
		idStr := strings.TrimPrefix(data, "reject_")

		err := RejectUser(database, bot, idStr, adminID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(adminID, "❌ Ошибка отклонения заявки."))
			return
		}

		newText := fmt.Sprintf("❌ Заявка отклонена.\nОтклонил: @%s", adminUsername)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, newText)
		bot.Send(edit)
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

func NotifyAdminsAboutScoreRequest(bot *tgbotapi.BotAPI, database *sql.DB, score models.Score, studentName string) {
	adminIDStr := os.Getenv("ADMIN_ID")
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		log.Println("Ошибка чтения ADMIN_ID:", err)
		return
	}

	categoryName, err := db.GetCategoryByID(database, int(score.CategoryID))
	if err != nil {
		log.Println("Ошибка получения категории:", err)
		return
	}
	action := "начисление"
	if score.Type == "remove" {
		action = "списание"
	}

	comment := "без комментария"
	if score.Comment != nil && *score.Comment != "" {
		comment = *score.Comment
	}

	student, err := db.GetUserByID(database, score.StudentID)
	if err != nil {
		log.Println("Ошибка получения ученика:", err)
		return
	}
	var classLetter string
	var classNumber int64
	if student.ClassLetter != nil || student.ClassNumber != nil {
		classLetter = *student.ClassLetter
		classNumber = *student.ClassNumber
	}
	msg := fmt.Sprintf("🆕 Новая заявка на %s:\n👤 Ученик: %s\n🏫 Класс: %d%s\n📚 Категория: %s\n🎯 Баллы: %d\n💬 Комментарий: %s\n\nОжидает подтверждения.",
		action, studentName, classNumber, classLetter, categoryName, score.Points, comment,
	)
	bot.Send(tgbotapi.NewMessage(adminID, msg))
}
