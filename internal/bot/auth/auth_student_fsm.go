package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type StudentFSMState string

const (
	StateStudentName           StudentFSMState = "student_name"
	StateStudentClassNum       StudentFSMState = "student_class_num"
	StateStudentLetterBtn      StudentFSMState = "student_class_letter_btn"
	StateStudentWaitingConfirm StudentFSMState = "student_waiting"
)

var (
	studentFSM  = make(map[int64]StudentFSMState)
	studentData = make(map[int64]*StudentRegisterData)
)

type StudentRegisterData struct {
	Name        string
	ClassNumber int64
	ClassLetter string
}

// ==== helpers ====
func studentBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("student_back", "student_cancel")
}

func studentClassNumberRowsFromDB(ctx context.Context, database *sql.DB) [][]tgbotapi.InlineKeyboardButton {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		return [][]tgbotapi.InlineKeyboardButton{
			studentBackCancelRow(),
		}
	}

	numsSet := make(map[int]struct{})
	for _, c := range classes {
		numsSet[c.Number] = struct{}{}
	}
	var nums []int
	for n := range numsSet {
		nums = append(nums, n)
	}
	// тут добавь
	// sort.Ints(nums)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, n := range nums {
		cb := fmt.Sprintf("student_class_num_%d", n)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", n), cb),
		))
	}
	rows = append(rows, studentBackCancelRow())
	return rows
}

func studentClassLetterRowsFromDB(ctx context.Context, database *sql.DB, number int) [][]tgbotapi.InlineKeyboardButton {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		return [][]tgbotapi.InlineKeyboardButton{
			studentBackCancelRow(),
		}
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range classes {
		if c.Number != number {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(strings.ToUpper(c.Letter), "student_class_letter_"+strings.ToUpper(c.Letter)),
		))
	}
	rows = append(rows, studentBackCancelRow())
	return rows
}

func studentEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	if _, err := tg.Send(bot, cfg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// StartStudentRegistration Начало FSM ученика
func StartStudentRegistration(ctx context.Context, chatID int64, bot *tgbotapi.BotAPI) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	delete(studentFSM, chatID)
	delete(studentData, chatID)

	studentFSM[chatID] = StateStudentName
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Введите ваше ФИО:")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// HandleStudentFSM Обработка шагов FSM
func HandleStudentFSM(ctx context.Context, chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	trimmed := strings.TrimSpace(msg)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	state := studentFSM[chatID]

	if state == StateStudentName {
		studentData[chatID] = &StudentRegisterData{Name: msg}
		studentFSM[chatID] = StateStudentClassNum

		rows := studentClassNumberRowsFromDB(ctx, database)

		msgOut := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		if _, err := tg.Send(bot, msgOut); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

func SaveStudentRequest(ctx context.Context, database *sql.DB, chatID int64, data *StudentRegisterData) (int64, error) {
	classID, err := db.ClassIDByNumberAndLetter(ctx, database, data.ClassNumber, data.ClassLetter)
	if err != nil {
		return 0, fmt.Errorf("❌ Ошибка: выбранный класс не существует. %w", err)
	}
	var newID int64
	if err := database.QueryRowContext(ctx, `
    	INSERT INTO users (telegram_id, name, role, class_id, class_number, class_letter, confirmed)
    	VALUES ($1,$2,'student',$3,$4,$5,FALSE)
    	RETURNING id
		`, chatID, data.Name, classID, data.ClassNumber, data.ClassLetter).Scan(&newID); err != nil {
		return 0, err
	}
	return newID, nil
}

func HandleStudentCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB) {
	chatID := cb.Message.Chat.ID
	data := cb.Data
	if data == "student_cancel" {
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if data == "student_back" {
		switch studentFSM[chatID] {
		case StateStudentClassNum:
			fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
			if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Введите ваше ФИО:")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			studentFSM[chatID] = StateStudentName
		case StateStudentLetterBtn:
			studentFSM[chatID] = StateStudentClassNum
			rows := studentClassNumberRowsFromDB(ctx, database)
			studentEditMenu(bot, chatID, cb.Message.MessageID, "Выберите номер класса:", rows)
		case StateStudentWaitingConfirm:
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Заявка уже отправлена, ожидайте подтверждения.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		default:
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "Действие недоступно на этом шаге.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		}
		return
	}

	if strings.HasPrefix(data, "student_class_num_") {
		numStr := strings.TrimPrefix(data, "student_class_num_")
		num, err := strconv.Atoi(numStr)
		if err != nil || num < 1 || num > 11 {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Некорректный номер класса.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if studentData[chatID] == nil {
			studentData[chatID] = &StudentRegisterData{}
		}
		studentData[chatID].ClassNumber = int64(num)
		studentFSM[chatID] = StateStudentLetterBtn
		rows := studentClassLetterRowsFromDB(ctx, database, num)
		studentEditMenu(bot, chatID, cb.Message.MessageID, "Выберите букву класса:", rows)
		return
	}

	if strings.HasPrefix(data, "student_class_letter_") {
		letter := strings.TrimPrefix(data, "student_class_letter_")
		studentData[chatID].ClassLetter = letter
		studentFSM[chatID] = StateStudentWaitingConfirm

		id, err := SaveStudentRequest(ctx, database, chatID, studentData[chatID])
		if err != nil {
			fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
			if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Ошибка при сохранении заявки. Попробуйте позже.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			delete(studentFSM, chatID)
			delete(studentData, chatID)
			return
		}
		fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Заявка на регистрацию отправлена администратору. Ожидайте подтверждения.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		handlers.NotifyAdminsAboutNewUser(ctx, bot, database, id)
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		return
	}
}
