package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type RecoveryFSMState string

const (
	StateRecoveryRole          RecoveryFSMState = "recovery_role"
	StateRecoveryName          RecoveryFSMState = "recovery_name"
	StateRecoveryStudentName   RecoveryFSMState = "recovery_student_name"
	StateRecoveryClassNumber   RecoveryFSMState = "recovery_class_number"
	StateRecoveryClassLetter   RecoveryFSMState = "recovery_class_letter"
	StateRecoveryWaitingReview RecoveryFSMState = "recovery_waiting_review"
)

type RecoveryData struct {
	Role        string
	Name        string
	StudentName string
	ClassNumber int64
	ClassLetter string
}

var (
	recoveryFSM  = make(map[int64]RecoveryFSMState)
	recoveryData = make(map[int64]*RecoveryData)
)

func GetRecoveryFSMState(chatID int64) RecoveryFSMState {
	return recoveryFSM[chatID]
}

func CancelRecovery(chatID int64) {
	delete(recoveryFSM, chatID)
	delete(recoveryData, chatID)
}

func StartAccountRecovery(ctx context.Context, chatID int64, bot *tgbotapi.BotAPI) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	CancelRecovery(chatID)
	recoveryFSM[chatID] = StateRecoveryRole
	recoveryData[chatID] = &RecoveryData{}

	msg := tgbotapi.NewMessage(chatID, "Выберите роль существующей учётной записи:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Ученик", "recovery_role_student"),
			tgbotapi.NewInlineKeyboardButtonData("Родитель", "recovery_role_parent"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Учитель", "recovery_role_teacher"),
			tgbotapi.NewInlineKeyboardButtonData("Администрация", "recovery_role_administration"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "recovery_cancel"),
		),
	)
	if _, err := tg.Send(bot, msg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func HandleRecoveryMessage(ctx context.Context, chatID int64, text string, bot *tgbotapi.BotAPI, database *sql.DB) {
	trimmed := strings.TrimSpace(text)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		CancelRecovery(chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Восстановление доступа отменено. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	data := recoveryData[chatID]
	if data == nil {
		CancelRecovery(chatID)
		return
	}

	switch recoveryFSM[chatID] {
	case StateRecoveryName:
		if trimmed == "" {
			return
		}
		data.Name = trimmed
		if data.Role == string(models.Parent) {
			recoveryFSM[chatID] = StateRecoveryStudentName
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Введите ФИО ребёнка, указанного в существующей учётной записи:")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if data.Role == string(models.Student) {
			recoveryFSM[chatID] = StateRecoveryClassNumber
			sendRecoveryClassNumbers(ctx, chatID, bot, database)
			return
		}
		submitRecovery(ctx, chatID, bot, database)
	case StateRecoveryStudentName:
		if trimmed == "" {
			return
		}
		data.StudentName = trimmed
		recoveryFSM[chatID] = StateRecoveryClassNumber
		sendRecoveryClassNumbers(ctx, chatID, bot, database)
	}
}

func HandleRecoveryCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data

	if data == "recovery_cancel" {
		CancelRecovery(chatID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "🚫 Восстановление доступа отменено. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	if strings.HasPrefix(data, "recovery_role_") {
		role := strings.TrimPrefix(data, "recovery_role_")
		switch role {
		case string(models.Student), string(models.Parent), string(models.Teacher), string(models.Administration):
		default:
			return
		}
		recoveryData[chatID] = &RecoveryData{Role: role}
		recoveryFSM[chatID] = StateRecoveryName
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Введите ваше ФИО так, как оно указано в существующей учётной записи:")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	state := recoveryFSM[chatID]
	rd := recoveryData[chatID]
	if rd == nil {
		return
	}

	if data == "recovery_back" {
		switch state {
		case StateRecoveryClassNumber:
			if rd.Role == string(models.Parent) {
				recoveryFSM[chatID] = StateRecoveryStudentName
				if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Введите ФИО ребёнка, указанного в существующей учётной записи:")); err != nil {
					metrics.HandlerErrors.Inc()
				}
			} else {
				recoveryFSM[chatID] = StateRecoveryName
				if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Введите ваше ФИО так, как оно указано в существующей учётной записи:")); err != nil {
					metrics.HandlerErrors.Inc()
				}
			}
		case StateRecoveryClassLetter:
			recoveryFSM[chatID] = StateRecoveryClassNumber
			editRecoveryClassNumbers(ctx, chatID, cb.Message.MessageID, bot, database)
		}
		return
	}

	if strings.HasPrefix(data, "recovery_class_num_") {
		n, err := strconv.ParseInt(strings.TrimPrefix(data, "recovery_class_num_"), 10, 64)
		if err != nil {
			return
		}
		rd.ClassNumber = n
		recoveryFSM[chatID] = StateRecoveryClassLetter
		editRecoveryClassLetters(ctx, chatID, cb.Message.MessageID, bot, database, n)
		return
	}

	if strings.HasPrefix(data, "recovery_class_letter_") {
		rd.ClassLetter = strings.TrimPrefix(data, "recovery_class_letter_")
		recoveryFSM[chatID] = StateRecoveryWaitingReview
		submitRecovery(ctx, chatID, bot, database)
	}
}

func submitRecovery(ctx context.Context, chatID int64, bot *tgbotapi.BotAPI, database *sql.DB) {
	rd := recoveryData[chatID]
	if rd == nil {
		CancelRecovery(chatID)
		return
	}

	target, err := db.FindAccountRecoveryTarget(ctx, database, db.AccountRecoveryLookup{
		Role:        rd.Role,
		Name:        rd.Name,
		StudentName: rd.StudentName,
		ClassNumber: rd.ClassNumber,
		ClassLetter: rd.ClassLetter,
	})
	if err != nil {
		switch {
		case errors.Is(err, db.ErrRecoveryAccountNotFound):
			sendRecoveryResult(bot, chatID, "❌ Учётная запись с указанными данными не найдена. Проверьте данные или зарегистрируйтесь как новый пользователь.")
		case errors.Is(err, db.ErrRecoveryAmbiguousAccount):
			sendRecoveryResult(bot, chatID, "⚠️ Найдено несколько подходящих учётных записей. Автоматически определить нужную запись невозможно. Обратитесь к администратору.")
		default:
			sendRecoveryResult(bot, chatID, "❌ Не удалось проверить учётную запись. Попробуйте позже.")
		}
		CancelRecovery(chatID)
		return
	}

	req, err := db.CreateAccountRecoveryRequest(ctx, database, target, chatID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrRecoveryTelegramInUse):
			sendRecoveryResult(bot, chatID, "⚠️ Этот Telegram ID уже привязан к учётной записи. Используйте /start для входа.")
		case errors.Is(err, db.ErrRecoveryPendingSame):
			sendRecoveryResult(bot, chatID, "⏳ Такая заявка на восстановление доступа уже отправлена и ожидает решения администратора.")
		case errors.Is(err, db.ErrRecoveryPendingExists):
			sendRecoveryResult(bot, chatID, "⚠️ По этой учётной записи уже есть другая заявка на восстановление. Обратитесь к администратору.")
		default:
			sendRecoveryResult(bot, chatID, "❌ Не удалось создать заявку на восстановление. Попробуйте позже.")
		}
		CancelRecovery(chatID)
		return
	}

	if err := handlers.NotifyAdminsAboutAccountRecovery(ctx, bot, database, req.ID); err != nil {
		sendRecoveryResult(bot, chatID, "✅ Заявка создана, но администратор мог не получить мгновенное уведомление. Заявка сохранена и ожидает обработки.")
	} else {
		sendRecoveryResult(bot, chatID, "✅ Заявка на восстановление доступа отправлена администратору. Ожидайте подтверждения.")
	}
	CancelRecovery(chatID)
}

func sendRecoveryResult(bot *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, text)); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func recoveryClassNumberRows(ctx context.Context, database *sql.DB) [][]tgbotapi.InlineKeyboardButton {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		return [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "recovery_back"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "recovery_cancel"),
			),
		}
	}

	numsSet := make(map[int]struct{})
	for _, c := range classes {
		numsSet[c.Number] = struct{}{}
	}
	nums := make([]int, 0, len(numsSet))
	for n := range numsSet {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(nums)+1)
	for _, n := range nums {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", n), fmt.Sprintf("recovery_class_num_%d", n)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "recovery_back"),
		tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "recovery_cancel"),
	))
	return rows
}

func recoveryClassLetterRows(ctx context.Context, database *sql.DB, number int64) [][]tgbotapi.InlineKeyboardButton {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		return [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "recovery_back"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "recovery_cancel"),
			),
		}
	}

	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	for _, c := range classes {
		if int64(c.Number) != number {
			continue
		}
		letter := strings.ToUpper(c.Letter)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(letter, "recovery_class_letter_"+letter),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "recovery_back"),
		tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "recovery_cancel"),
	))
	return rows
}

func sendRecoveryClassNumbers(ctx context.Context, chatID int64, bot *tgbotapi.BotAPI, database *sql.DB) {
	msg := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(recoveryClassNumberRows(ctx, database)...)
	if _, err := tg.Send(bot, msg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func editRecoveryClassNumbers(ctx context.Context, chatID int64, messageID int, bot *tgbotapi.BotAPI, database *sql.DB) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "Выберите номер класса:")
	markup := tgbotapi.NewInlineKeyboardMarkup(recoveryClassNumberRows(ctx, database)...)
	edit.ReplyMarkup = &markup
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func editRecoveryClassLetters(ctx context.Context, chatID int64, messageID int, bot *tgbotapi.BotAPI, database *sql.DB, number int64) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, "Выберите букву класса:")
	markup := tgbotapi.NewInlineKeyboardMarkup(recoveryClassLetterRows(ctx, database, number)...)
	edit.ReplyMarkup = &markup
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}
