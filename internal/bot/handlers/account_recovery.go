package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func NotifyAdminsAboutAccountRecovery(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, requestID int64) error {
	req, err := db.GetAccountRecoveryRequest(ctx, database, requestID)
	if err != nil {
		return err
	}

	text := formatAccountRecoveryCard(req)
	approve := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить замену", fmt.Sprintf("recovery_approve_%d", requestID))
	reject := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("recovery_reject_%d", requestID))
	markup := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(approve, reject))

	rows, err := database.QueryContext(ctx, `
SELECT telegram_id
FROM users
WHERE role = 'admin' AND confirmed = TRUE AND is_active = TRUE
ORDER BY id
`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	sent := 0
	for rows.Next() {
		var adminTG int64
		if err := rows.Scan(&adminTG); err != nil {
			continue
		}
		if !db.IsAdminID(adminTG) {
			continue
		}
		msg := tgbotapi.NewMessage(adminTG, text)
		msg.ReplyMarkup = markup
		if _, err := tg.Send(bot, msg); err != nil {
			metrics.HandlerErrors.Inc()
			log.Printf("NotifyAdminsAboutAccountRecovery: send to %d failed: %v", adminTG, err)
			continue
		}
		sent++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if sent == 0 {
		return fmt.Errorf("no active admins received account recovery request")
	}
	return nil
}

func ShowPendingAccountRecoveryRequests(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, adminTelegramID int64) {
	if !db.IsAdminID(adminTelegramID) {
		return
	}
	rows, err := database.QueryContext(ctx, `
SELECT id
FROM account_recovery_requests
WHERE status = 'pending'
ORDER BY requested_at ASC, id ASC
`)
	if err != nil {
		if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(adminTelegramID, "❌ Ошибка при получении заявок на восстановление доступа.")); sendErr != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}

	for _, id := range ids {
		req, err := db.GetAccountRecoveryRequest(ctx, database, id)
		if err != nil {
			continue
		}
		approve := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить замену", fmt.Sprintf("recovery_approve_%d", id))
		reject := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("recovery_reject_%d", id))
		msg := tgbotapi.NewMessage(adminTelegramID, formatAccountRecoveryCard(req))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(approve, reject))
		if _, sendErr := tg.Send(bot, msg); sendErr != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

func HandleAccountRecoveryAdminCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	if cb.From == nil || !db.IsAdminID(cb.From.ID) {
		if cb.Message != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(cb.Message.Chat.ID, "⛔ Недостаточно прав для обработки этой заявки.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		}
		return
	}
	if cb.Message == nil {
		return
	}

	var (
		requestID int64
		action    string
	)
	switch {
	case strings.HasPrefix(cb.Data, "recovery_approve_"):
		action = "approve"
		id, err := strconv.ParseInt(strings.TrimPrefix(cb.Data, "recovery_approve_"), 10, 64)
		if err != nil {
			return
		}
		requestID = id
	case strings.HasPrefix(cb.Data, "recovery_reject_"):
		action = "reject"
		id, err := strconv.ParseInt(strings.TrimPrefix(cb.Data, "recovery_reject_"), 10, 64)
		if err != nil {
			return
		}
		requestID = id
	default:
		return
	}

	var req *db.AccountRecoveryRequest
	var err error
	if action == "approve" {
		req, err = db.ApproveAccountRecoveryRequest(ctx, database, requestID, cb.From.ID)
	} else {
		req, err = db.RejectAccountRecoveryRequest(ctx, database, requestID, cb.From.ID)
	}
	if err != nil {
		text := "❌ Не удалось обработать заявку."
		switch {
		case errors.Is(err, db.ErrRecoveryRequestNotFound):
			text = "⚠️ Эта заявка уже обработана или не существует."
		case errors.Is(err, db.ErrRecoveryRequestStale):
			text = "⚠️ Заявка устарела: Telegram ID учётной записи уже изменился."
		case errors.Is(err, db.ErrRecoveryTelegramInUse):
			text = "⚠️ Новый Telegram ID уже привязан к другой учётной записи."
		case errors.Is(err, db.ErrRecoveryAdminForbidden):
			text = "⛔ Недостаточно прав для обработки этой заявки."
		}
		if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(cb.Message.Chat.ID, text)); sendErr != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	username := cb.From.UserName
	adminLabel := fmt.Sprintf("Telegram ID %d", cb.From.ID)
	if username != "" {
		adminLabel = "@" + username
	}

	var edited string
	if action == "approve" {
		db.ClearUserFSMRole(req.OldTelegramID)
		edited = fmt.Sprintf("✅ Доступ восстановлен.\n\n%s\n\nПодтвердил: %s", formatAccountRecoveryCard(req), adminLabel)
		userMsg := tgbotapi.NewMessage(req.NewTelegramID, "✅ Ваша заявка подтверждена. Доступ к существующей учётной записи восстановлен. Нажмите /start для входа.")
		if !req.IsActive {
			userMsg.Text = "✅ Telegram ID учётной записи обновлён. Учётная запись сейчас неактивна, поэтому доступ к функциям бота остаётся закрыт. Обратитесь к администратору."
		}
		if _, sendErr := tg.Send(bot, userMsg); sendErr != nil {
			metrics.HandlerErrors.Inc()
		}
	} else {
		edited = fmt.Sprintf("❌ Заявка на восстановление отклонена.\n\n%s\n\nОтклонил: %s", formatAccountRecoveryCard(req), adminLabel)
		if _, sendErr := tg.Send(bot, tgbotapi.NewMessage(req.NewTelegramID, "❌ Ваша заявка на восстановление доступа отклонена. Пройдите регистрацию заново или обратитесь к администратору.")); sendErr != nil {
			metrics.HandlerErrors.Inc()
		}
	}

	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, edited)
	if _, sendErr := tg.Send(bot, edit); sendErr != nil {
		metrics.HandlerErrors.Inc()
	}
}

func formatAccountRecoveryCard(req *db.AccountRecoveryRequest) string {
	roleLabel := map[string]string{
		"student":        "Ученик",
		"parent":         "Родитель",
		"teacher":        "Учитель",
		"administration": "Администрация",
	}[req.Role]
	if roleLabel == "" {
		roleLabel = req.Role
	}

	lines := []string{
		"🔑 Запрос на восстановление доступа",
		"",
		fmt.Sprintf("👤 %s", req.Name),
		fmt.Sprintf("🧩 Роль: %s", roleLabel),
	}
	if req.Role == "student" && req.ClassNumber != nil && req.ClassLetter != nil {
		lines = append(lines, fmt.Sprintf("🏫 Класс: %d%s", *req.ClassNumber, *req.ClassLetter))
	}
	if req.Role == "parent" && req.StudentName != nil {
		lines = append(lines, fmt.Sprintf("👦 Ребёнок: %s", *req.StudentName))
		if req.StudentClassNumber != nil && req.StudentClassLetter != nil {
			lines = append(lines, fmt.Sprintf("🏫 Класс ребёнка: %d%s", *req.StudentClassNumber, *req.StudentClassLetter))
		}
	}

	active := "✅ активна"
	if !req.IsActive {
		active = "🚫 неактивна"
	}
	lines = append(lines,
		"",
		fmt.Sprintf("ID учётной записи: %d", req.UserID),
		fmt.Sprintf("Старый Telegram ID: %d", req.OldTelegramID),
		fmt.Sprintf("Новый Telegram ID: %d", req.NewTelegramID),
		fmt.Sprintf("Учётная запись: %s", active),
	)
	return strings.Join(lines, "\n")
}
