package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var notifiedAdmins = make(map[int64]bool)

func ShowPendingUsers(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	adminIDStr := os.Getenv("ADMIN_ID")
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		log.Println("Ошибка при чтении ADMIN_ID из .env:", err)
		return
	}

	var count int
	err = database.QueryRow(`SELECT COUNT(*) FROM users WHERE confirmed = 0 AND role != 'admin'`).Scan(&count)
	if err != nil {
		log.Println("Ошибка при подсчете заявок:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при проверке заявок."))
		return
	}

	if count == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Нет ожидающих подтверждения заявок."))
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

		btnYes := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_user_%d", id))
		brnNo := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_user_%d", id))
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

	if strings.HasPrefix(data, "confirm_user_") {
		idStr := strings.TrimPrefix(data, "confirm_user_")

		err := ConfirmUser(database, bot, idStr, adminID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(adminID, "❌ Ошибка подтверждения заявки."))
			return
		}

		newText := fmt.Sprintf("✅ Заявка подтверждена.\nПодтвердил: @%s", adminUsername)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, newText)
		bot.Send(edit)
	} else if strings.HasPrefix(data, "reject_user_") {
		idStr := strings.TrimPrefix(data, "reject_user_")

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
	action := "начисления"
	if score.Type == "remove" {
		action = "списания"
	}

	// 📢 Получаем всех админов и администрацию
	rows, err := database.Query(`SELECT telegram_id FROM users WHERE role IN ('admin', 'administration') AND confirmed = 1 AND is_active = 1`)
	if err != nil {
		log.Println("❌ Ошибка при получении списка админов:", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var tgID int64
		if err := rows.Scan(&tgID); err != nil {
			log.Println("❌ Ошибка чтения telegram_id:", err)
			continue
		}

		// Отправим уведомление только один раз
		if !notifiedAdmins[tgID] {
			notifiedAdmins[tgID] = true
			msg := tgbotapi.NewMessage(tgID, fmt.Sprintf("📥 Появились новые заявки для подтверждения %s.", action))
			bot.Send(msg)
		}
	}
}

// Показываем заявки на привязку "родитель ⇄ ребёнок"
func ShowPendingParentLinks(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	rows, err := database.Query(`
        SELECT r.id, p.name as parent_name, s.name as student_name, s.class_number, s.class_letter
        FROM parent_link_requests r
        JOIN users p ON p.id = r.parent_id
        JOIN users s ON s.id = r.student_id
        ORDER BY r.created_at ASC
    `)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при получении заявок на привязку."))
		return
	}
	defer rows.Close()

	has := false
	for rows.Next() {
		has = true
		var id int
		var parentName, studentName, classLetter string
		var classNumber sql.NullString // если у вас integer — используйте int
		// подстройте типы под вашу схему
		if err := rows.Scan(&id, &parentName, &studentName, &classNumber, &classLetter); err != nil {
			continue
		}
		msg := fmt.Sprintf("Заявка на привязку:\n👤 Родитель: %s\n👦 Ребёнок: %s\n🏫 Класс: %s%s",
			parentName, studentName, classNumber.String, classLetter,
		)
		markup := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_link_%d", id)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_link_%d", id)),
			),
		)
		m := tgbotapi.NewMessage(chatID, msg)
		m.ReplyMarkup = markup
		bot.Send(m)
	}
	if !has {
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Нет заявок на привязку детей."))
	}
}

// Обработка коллбеков по заявкам на привязку
func HandleParentLinkApprovalCallback(cb *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB, adminID int64) {
	data := cb.Data
	chatID := cb.Message.Chat.ID
	msgID := cb.Message.MessageID
	adminUsername := cb.From.UserName

	getIDs := func(reqID string) (parentID, studentID int64, err error) {
		err = database.QueryRow(`SELECT parent_id, student_id FROM parent_link_requests WHERE id = ?`, reqID).
			Scan(&parentID, &studentID)
		return
	}

	if strings.HasPrefix(data, "confirm_link_") {
		reqID := strings.TrimPrefix(data, "confirm_link_")
		parentID, studentID, err := getIDs(reqID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Заявка не найдена."))
			return
		}

		tx, err := database.Begin()
		if err != nil {
			return
		}
		defer tx.Rollback()

		// Создаём связь (id в users, не telegram_id!)
		if _, err = tx.Exec(`INSERT OR IGNORE INTO parents_students(parent_id, student_id) VALUES(?,?)`, parentID, studentID); err != nil {
			return
		}
		if _, err = tx.Exec(`DELETE FROM parent_link_requests WHERE id = ?`, reqID); err != nil {
			return
		}
		if err = tx.Commit(); err != nil {
			return
		}

		// Уведомления
		var pTG, sTG int64
		_ = database.QueryRow(`SELECT telegram_id FROM users WHERE id = ?`, parentID).Scan(&pTG)
		_ = database.QueryRow(`SELECT telegram_id FROM users WHERE id = ?`, studentID).Scan(&sTG)
		if pTG != 0 {
			bot.Send(tgbotapi.NewMessage(pTG, "✅ Привязка к ребёнку подтверждена администратором."))
		}
		if sTG != 0 {
			bot.Send(tgbotapi.NewMessage(sTG, "ℹ️ Ваш родитель привязан в системе."))
		}

		edit := tgbotapi.NewEditMessageText(chatID, msgID, fmt.Sprintf("✅ Заявка на привязку подтверждена.\nПодтвердил: @%s", adminUsername))
		bot.Send(edit)
		bot.Request(tgbotapi.NewCallback(cb.ID, "Готово"))
		return
	}

	if strings.HasPrefix(data, "reject_link_") {
		reqID := strings.TrimPrefix(data, "reject_link_")
		var parentID int64
		_ = database.QueryRow(`SELECT parent_id FROM parent_link_requests WHERE id = ?`, reqID).Scan(&parentID)
		_, _ = database.Exec(`DELETE FROM parent_link_requests WHERE id = ?`, reqID)

		// Уведомим родителя
		if parentID != 0 {
			var pTG int64
			_ = database.QueryRow(`SELECT telegram_id FROM users WHERE id = ?`, parentID).Scan(&pTG)
			if pTG != 0 {
				bot.Send(tgbotapi.NewMessage(pTG, "❌ Заявка на привязку отклонена администратором."))
			}
		}

		edit := tgbotapi.NewEditMessageText(chatID, msgID, fmt.Sprintf("❌ Заявка на привязку отклонена.\nОтклонил: @%s", adminUsername))
		bot.Send(edit)
		bot.Request(tgbotapi.NewCallback(cb.ID, "Готово"))
		return
	}
}

// Уведомление админам о новой заявке на привязку
func NotifyAdminsAboutParentLink(bot *tgbotapi.BotAPI, database *sql.DB, requestID int64) {
	rows, err := database.Query(`SELECT telegram_id FROM users WHERE role = 'admin' AND confirmed = 1 AND is_active = 1`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var tgID int64
		if err := rows.Scan(&tgID); err != nil {
			continue
		}
		text := fmt.Sprintf("📥 Новая заявка на привязку ребёнка. Откройте «📥 Заявки на авторизацию», чтобы обработать.")
		// Можно отправлять сразу карточки (ShowPendingParentLinks), но обычно делаем по кнопке в меню
		bot.Send(tgbotapi.NewMessage(tgID, text))
	}
}
