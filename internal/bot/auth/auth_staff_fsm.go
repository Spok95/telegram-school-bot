package auth

import (
	"context"
	"database/sql"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type StaffFSMState string

const (
	StateStaffName StaffFSMState = "staff_name"
	StateStaffWait StaffFSMState = "staff_wait"
)

var (
	staffFSM  = make(map[int64]StaffFSMState)
	staffData = make(map[int64]string)
)

func StartStaffRegistration(chatID int64, bot *tgbotapi.BotAPI) {
	staffFSM[chatID] = StateStaffName
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Введите ваше ФИО:")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func HandleStaffFSM(ctx context.Context, chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB, role string) {
	trimmed := strings.TrimSpace(msg)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(staffFSM, chatID)
		delete(staffData, chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	state := staffFSM[chatID]

	if state == StateStaffName {
		staffData[chatID] = msg
		staffFSM[chatID] = StateStaffWait

		id, err := SaveStaffRequest(ctx, database, chatID, msg, role)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Ошибка при сохранении заявки. Попробуйте позже.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			delete(staffFSM, chatID)
			delete(staffData, chatID)
			return
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Заявка на регистрацию отправлена администратору. Ожидайте подтверждения.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		handlers.NotifyAdminsAboutNewUser(ctx, bot, database, id)

		delete(staffFSM, chatID)
		delete(staffData, chatID)
	}
}

func SaveStaffRequest(ctx context.Context, database *sql.DB, telegramID int64, name, role string) (int64, error) {
	var id int64
	err := database.QueryRowContext(ctx, `
		INSERT INTO users (telegram_id, name, role, confirmed)
		VALUES ($1,$2,$3,FALSE)
		RETURNING id
		`, telegramID, name, role).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
