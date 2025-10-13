package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/menu"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var notifiedAdmins = make(map[int64]bool)

func ShowPendingUsers(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	var adminID int64
	if db.IsAdminID(chatID) {
		adminID = chatID
	}

	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE confirmed = FALSE AND role != 'admin'`).Scan(&count)
	if err != nil {
		log.Println("Ошибка при подсчете заявок:", err)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Ошибка при проверке заявок.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	if count == 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Нет ожидающих подтверждения заявок.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	rows, err := database.QueryContext(ctx, `
		SELECT id, name, role, telegram_id FROM users WHERE confirmed = FALSE AND role != 'admin'
	`)
	if err != nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(adminID, "Ошибка при получении заявок.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id int
		var name, role string
		var tgID int64

		if err := rows.Scan(&id, &name, &role, &tgID); err != nil {
			continue
		}

		var msg string

		switch role {
		case "student":
			var classNumber, classLetter sql.NullString
			err := database.QueryRowContext(ctx, `SELECT class_number, class_letter FROM users WHERE id = $1`, id).Scan(&classNumber, &classLetter)
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
		case "parent":
			var studentName, studentClassNumber, studentClassLetter sql.NullString

			// получаем имя родителя (Telegram username или имя из Telegram профиля, если есть)
			err := database.QueryRowContext(ctx, `
			SELECT u.name, u.class_number, u.class_letter
			FROM users u
			JOIN parents_students ps ON ps.student_id = u.id
			WHERE ps.parent_id = $1
		`, id).Scan(&studentName, &studentClassNumber, &studentClassLetter)
			if err != nil {
				log.Println("Ошибка при получении информации о ребёнке:", err)
				continue
			}

			msg = fmt.Sprintf(
				"Заявка на авторизацию:\n👤 Родитель: %s\n👦 Ребёнок: %s\n🏫 Класс: %s%s\n🧩 Роль: %s",
				name, studentName.String, studentClassNumber.String, studentClassLetter.String, role,
			)
		default:
			// fallback
			msg = fmt.Sprintf("Заявка:\n👤 %s\n🧩 Роль: %s\nTelegramID: %d", name, role, tgID)
		}

		btnYes := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", id))
		brnNo := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", id))
		markup := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btnYes, brnNo))

		message := tgbotapi.NewMessage(adminID, msg)
		message.ReplyMarkup = markup
		if _, err := tg.Send(bot, message); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

func HandleAdminCallback(ctx context.Context, callback *tgbotapi.CallbackQuery, database *sql.DB, bot *tgbotapi.BotAPI, adminID int64) {
	data := callback.Data
	messageID := callback.Message.MessageID
	chatID := callback.Message.Chat.ID
	adminUsername := callback.From.UserName

	if strings.HasPrefix(data, "confirm_") {
		idStr := strings.TrimPrefix(data, "confirm_")

		err := ConfirmUser(ctx, database, bot, idStr, adminID)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(adminID, "❌ Ошибка подтверждения заявки.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		newText := fmt.Sprintf("✅ Заявка подтверждена.\nПодтвердил: @%s", adminUsername)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, newText)
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
	} else if strings.HasPrefix(data, "reject_") {
		idStr := strings.TrimPrefix(data, "reject_")

		err := RejectUser(ctx, database, bot, idStr)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(adminID, "❌ Ошибка отклонения заявки.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		newText := fmt.Sprintf("❌ Заявка отклонена.\nОтклонил: @%s", adminUsername)
		edit := tgbotapi.NewEditMessageText(chatID, messageID, newText)
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(adminID, "Обработано")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func ConfirmUser(ctx context.Context, database *sql.DB, bot *tgbotapi.BotAPI, name string, adminTG int64) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var telegramID int64
	err = tx.QueryRowContext(ctx, `SELECT telegram_id FROM users WHERE id = $1`, name).Scan(&telegramID)
	if err != nil {
		return err
	}

	// Получаем текущую роль (до подтверждения)
	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = $1 AND confirmed = FALSE`, name).Scan(&role)
	if err != nil {
		// либо уже подтверждён, либо не найден
		return fmt.Errorf("заявка не найдена или уже обработана")
	}

	// Подтверждаем, только если ещё не подтверждён
	res, err := tx.ExecContext(ctx, `UPDATE users SET confirmed = TRUE WHERE id = $1 AND confirmed = FALSE`, name)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("заявка уже подтверждена другим админом")
	}

	var adminID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE telegram_id = $1 AND role = 'admin'`, adminTG).Scan(&adminID); err != nil {
		// если вдруг админ не заведен в users — можно записать NULL/0 или убрать FK, но лучше завести админа
		return fmt.Errorf("администратор не найден в users: %w", err)
	}

	// Фиксируем в истории
	_, err = tx.ExecContext(ctx, `
		INSERT INTO role_changes (user_id, old_role, new_role, changed_by, changed_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
	`, name, "unconfirmed", role, adminID)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	msg := tgbotapi.NewMessage(telegramID, "✅ Ваша заявка подтверждена. Добро пожаловать!")
	msg.ReplyMarkup = menu.GetRoleMenu(role)
	if _, err := tg.Send(bot, msg); err != nil {
		metrics.HandlerErrors.Inc()
	}

	return nil
}

func RejectUser(ctx context.Context, database *sql.DB, bot *tgbotapi.BotAPI, name string) error {
	var telegramID int64
	err := database.QueryRowContext(ctx, `SELECT telegram_id FROM users WHERE id = $1`, name).Scan(&telegramID)
	if err != nil {
		return err
	}

	_, err = database.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, name)
	if err != nil {
		return err
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(telegramID, "❌ Ваша заявка отклонена. Попробуйте позже или обратитесь к администратору.")); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return nil
}

// NotifyAdminsAboutNewUser уведомление админам о новой заявке на авторизацию пользователя
func NotifyAdminsAboutNewUser(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, userID int64) {
	// читаем профиль со всем, что нужно для карточки
	var (
		name, role         string
		tgID               int64
		classNum, classLet sql.NullString
	)
	if err := database.QueryRowContext(ctx, `
		SELECT name, role, telegram_id, class_number, class_letter
		FROM users
		WHERE id = $1
		`, userID).Scan(&name, &role, &tgID, &classNum, &classLet); err != nil {
		log.Printf("NotifyAdminsAboutNewUser: запись %d ещё не готова: %v", userID, err)
		return
	}

	// формируем текст
	var msg string
	switch role {
	case "parent":
		var sName, sNum, sLet sql.NullString
		_ = database.QueryRowContext(ctx, `
         SELECT s.name, s.class_number, s.class_letter
         FROM parents_students ps
         JOIN users s ON s.id = ps.student_id
         WHERE ps.parent_id = $1
         LIMIT 1
     `, userID).Scan(&sName, &sNum, &sLet)
		msg = fmt.Sprintf("Заявка на авторизацию:\n👤 Родитель: %s\n👦 Ребёнок: %s\n🏫 Класс: %s%s\n🧩 Роль: %s",
			name, sName.String, sNum.String, sLet.String, role,
		)
	case "student":
		if classNum.Valid && classLet.Valid {
			msg = fmt.Sprintf("Заявка на авторизацию:\n👤 %s\n🏫 Класс: %s%s\n🧩 Роль: %s",
				name, classNum.String, classLet.String, role,
			)
		} else {
			msg = fmt.Sprintf("Заявка на авторизацию:\n👤 %s\n🧩 Роль: %s", name, role)
		}
	default:
		msg = fmt.Sprintf("Заявка на авторизацию:\n👤 %s\n🧩 Роль: %s", name, role)
	}

	// кнопки подтверждения/отклонения такие же, как в ShowPendingUsers
	btnYes := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_%d", userID))
	btnNo := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("reject_%d", userID))
	markup := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btnYes, btnNo))

	// уведомляем всех админов
	rows, err := database.QueryContext(ctx, `SELECT telegram_id FROM users WHERE role = 'admin' AND confirmed = TRUE AND is_active = TRUE`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var adminTG int64
		if err := rows.Scan(&adminTG); err != nil {
			continue
		}
		m := tgbotapi.NewMessage(adminTG, msg)
		m.ReplyMarkup = markup
		if _, err := tg.Send(bot, m); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

func NotifyAdminsAboutScoreRequest(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, score models.Score) {
	action := "начисления"
	if score.Type == "remove" {
		action = "списания"
	}

	// 📢 Получаем всех админов и администрацию
	rows, err := database.QueryContext(ctx, `SELECT telegram_id FROM users WHERE role IN ('admin', 'administration') AND confirmed = TRUE AND is_active = TRUE`)
	if err != nil {
		log.Println("❌ Ошибка при получении списка админов:", err)
		return
	}
	defer func() { _ = rows.Close() }()

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
			if _, err := tg.Send(bot, msg); err != nil {
				metrics.HandlerErrors.Inc()
			}
		}
	}
}

// ShowPendingParentLinks показывает заявки на привязку "родитель ⇄ ребёнок"
func ShowPendingParentLinks(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64) {
	rows, err := database.QueryContext(ctx, `
        SELECT r.id, p.name as parent_name, s.name as student_name, s.class_number, s.class_letter
        FROM parent_link_requests r
        JOIN users p ON p.id = r.parent_id
        JOIN users s ON s.id = r.student_id
        ORDER BY r.created_at ASC
    `)
	if err != nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Ошибка при получении заявок на привязку.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { _ = rows.Close() }()

	has := false
	for rows.Next() {
		has = true
		var id int
		var parentName, studentName, classLetter string
		var classNumber sql.NullString
		// подгоняем типы под нашу схему
		if err := rows.Scan(&id, &parentName, &studentName, &classNumber, &classLetter); err != nil {
			continue
		}
		msg := fmt.Sprintf("Заявка на привязку:\n👤 Родитель: %s\n👦 Ребёнок: %s\n🏫 Класс: %s%s",
			parentName, studentName, classNumber.String, classLetter,
		)
		markup := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("link_confirm_%d", id)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("link_reject_%d", id)),
			),
		)
		m := tgbotapi.NewMessage(chatID, msg)
		m.ReplyMarkup = markup
		if _, err := tg.Send(bot, m); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
	if !has {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "✅ Нет заявок на привязку детей.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

// HandleParentLinkApprovalCallback обработка коллбеков по заявкам на привязку
func HandleParentLinkApprovalCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB) {
	data := cb.Data
	chatID := cb.Message.Chat.ID
	msgID := cb.Message.MessageID
	adminUsername := cb.From.UserName

	getIDs := func(reqID string) (parentID, studentID int64, err error) {
		err = database.QueryRowContext(ctx, `SELECT parent_id, student_id FROM parent_link_requests WHERE id = $1`, reqID).
			Scan(&parentID, &studentID)
		return parentID, studentID, err
	}

	if strings.HasPrefix(data, "link_confirm_") {
		reqID := strings.TrimPrefix(data, "link_confirm_")
		parentID, studentID, err := getIDs(reqID)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Заявка не найдена.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		tx, err := database.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			return
		}
		defer func() { _ = tx.Rollback() }()

		// Создаём связь (id в users, не telegram_id!)
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO parents_students(parent_id, student_id)
			VALUES($1,$2)
			ON CONFLICT (parent_id, student_id) DO NOTHING
			`, parentID, studentID); err != nil {
			return
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM parent_link_requests WHERE id = $1`, reqID); err != nil {
			return
		}
		if err = tx.Commit(); err != nil {
			return
		}

		// ⤵️ После успешного создания связи пересчитываем активность родителя:
		// если у него теперь есть активные дети — он станет активным; если нет — останется неактивным.
		if err := db.RefreshParentActiveFlag(ctx, database, parentID); err != nil {
			log.Printf("не удалось обновить активность родителя: %v", err)
		}

		// (Опционально) если нужно запретить привязку к неактивному ученику на админском уровне,
		// раскомментировать блок ниже. Сейчас мы допускаем привязку и просто корректно считаем статус родителя.
		/*
			var active bool
			if err := database.QueryRowContext(ctx, `SELECT is_active FROM users WHERE id = $1`, studentID).Scan(&active); err == nil && !active {
			// Можно отправить инфо-заметку админу
				tg.Send(bot, tgbotapi.NewMessage(chatID, "ℹ️ Внимание: привязанный ребёнок неактивен. Родитель останется неактивным до появления активных детей."))
			}
		*/

		// Уведомления
		var pTG, sTG int64
		_ = database.QueryRowContext(ctx, `SELECT telegram_id FROM users WHERE id = $1`, parentID).Scan(&pTG)
		_ = database.QueryRowContext(ctx, `SELECT telegram_id FROM users WHERE id = $1`, studentID).Scan(&sTG)
		if pTG != 0 {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(pTG, "✅ Привязка к ребёнку подтверждена администратором.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		}
		if sTG != 0 {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(sTG, "ℹ️ Ваш родитель привязан в системе.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		}

		edit := tgbotapi.NewEditMessageText(chatID, msgID, fmt.Sprintf("✅ Заявка на привязку подтверждена.\nПодтвердил: @%s", adminUsername))
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Готово")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	if strings.HasPrefix(data, "link_reject_") {
		reqID := strings.TrimPrefix(data, "link_reject_")
		var parentID int64
		_ = database.QueryRowContext(ctx, `SELECT parent_id FROM parent_link_requests WHERE id = $1`, reqID).Scan(&parentID)
		_, _ = database.ExecContext(ctx, `DELETE FROM parent_link_requests WHERE id = $1`, reqID)

		// Уведомим родителя
		if parentID != 0 {
			var pTG int64
			_ = database.QueryRowContext(ctx, `SELECT telegram_id FROM users WHERE id = $1`, parentID).Scan(&pTG)
			if pTG != 0 {
				if _, err := tg.Send(bot, tgbotapi.NewMessage(pTG, "❌ Заявка на привязку отклонена администратором.")); err != nil {
					metrics.HandlerErrors.Inc()
				}
			}
		}

		edit := tgbotapi.NewEditMessageText(chatID, msgID, fmt.Sprintf("❌ Заявка на привязку отклонена.\nОтклонил: @%s", adminUsername))
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Готово")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
}

// NotifyAdminsAboutParentLink уведомление админам о новой заявке на привязку
func NotifyAdminsAboutParentLink(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB) {
	rows, err := database.QueryContext(ctx, `SELECT telegram_id FROM users WHERE role = 'admin' AND confirmed = TRUE AND is_active = TRUE`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var tgID int64
		if err := rows.Scan(&tgID); err != nil {
			continue
		}
		text := "📥 Новая заявка на привязку ребёнка. Откройте «📥 Заявки на авторизацию», чтобы обработать."
		// Можно отправлять сразу карточки (ShowPendingParentLinks), но обычно делаем по кнопке в меню
		if _, err := tg.Send(bot, tgbotapi.NewMessage(tgID, text)); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}
