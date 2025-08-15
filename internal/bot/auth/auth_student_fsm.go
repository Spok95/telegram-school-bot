package auth

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type StudentFSMState string

const (
	StateStudentName           StudentFSMState = "student_name"
	StateStudentClassNum       StudentFSMState = "student_class_num"
	StateStudentLetterBtn      StudentFSMState = "student_class_letter_btn"
	StateStudentWaitingConfirm StudentFSMState = "student_waiting"
)

var studentFSM = make(map[int64]StudentFSMState)
var studentData = make(map[int64]*StudentRegisterData)

type StudentRegisterData struct {
	Name        string
	ClassNumber int64
	ClassLetter string
}

// ==== helpers ====
func studentBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("student_back", "student_cancel")
}
func studentClassNumberRows() [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		cb := fmt.Sprintf("student_class_num_%d", i)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", i), cb),
		))
	}
	rows = append(rows, studentBackCancelRow())
	return rows
}
func studentClassLetterRows() [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l, "student_class_letter_"+l),
		))
	}
	rows = append(rows, studentBackCancelRow())
	return rows
}
func studentEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	bot.Send(cfg)
}

// Начало FSM ученика
func StartStudentRegistration(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	delete(studentFSM, chatID)
	delete(studentData, chatID)

	studentFSM[chatID] = StateStudentName
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ваше ФИО:"))
}

// Обработка шагов FSM
func HandleStudentFSM(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	trimmed := strings.TrimSpace(msg)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново."))
		return
	}

	state := studentFSM[chatID]

	switch state {
	case StateStudentName:
		studentData[chatID] = &StudentRegisterData{Name: msg}
		studentFSM[chatID] = StateStudentClassNum
		msgOut := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(studentClassNumberRows()...)
		bot.Send(msgOut)
	}
}

func SaveStudentRequest(database *sql.DB, chatID int64, data *StudentRegisterData) (int64, error) {
	classID, err := db.ClassIDByNumberAndLetter(database, data.ClassNumber, data.ClassLetter)
	if err != nil {
		return 0, fmt.Errorf("❌ Ошибка: выбранный класс не существует. %w", err)
	}
	var newID int64
	if err := database.QueryRow(`
    	INSERT INTO users (telegram_id, name, role, class_id, class_number, class_letter, confirmed)
    	VALUES ($1,$2,'student',$3,$4,$5,FALSE)
    	RETURNING id
		`, chatID, data.Name, classID, data.ClassNumber, data.ClassLetter).Scan(&newID); err != nil {
		return 0, err
	}
	return newID, nil
}

func HandleStudentCallback(cb *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB) {
	chatID := cb.Message.Chat.ID
	data := cb.Data
	if data == "student_cancel" {
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново."))
		return
	}
	if data == "student_back" {
		switch studentFSM[chatID] {
		case StateStudentClassNum:
			fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
			bot.Send(tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Введите ваше ФИО:"))
			studentFSM[chatID] = StateStudentName
		case StateStudentLetterBtn:
			studentFSM[chatID] = StateStudentClassNum
			studentEditMenu(bot, chatID, cb.Message.MessageID, "Выберите номер класса:", studentClassNumberRows())
		case StateStudentWaitingConfirm:
			bot.Request(tgbotapi.NewCallback(cb.ID, "Заявка уже отправлена, ожидайте подтверждения."))
		default:
			bot.Request(tgbotapi.NewCallback(cb.ID, "Действие недоступно на этом шаге."))
		}
		return
	}

	if strings.HasPrefix(data, "student_class_num_") {
		numStr := strings.TrimPrefix(data, "student_class_num_")
		num, err := strconv.Atoi(numStr)
		if err != nil || num < 1 || num > 11 {
			bot.Send(tgbotapi.NewMessage(chatID, "Некорректный номер класса."))
			return
		}
		if studentData[chatID] == nil {
			studentData[chatID] = &StudentRegisterData{}
		}
		studentData[chatID].ClassNumber = int64(num)
		studentFSM[chatID] = StateStudentLetterBtn
		studentEditMenu(bot, chatID, cb.Message.MessageID, "Выберите букву класса:", studentClassLetterRows())
		return
	}

	if strings.HasPrefix(data, "student_class_letter_") {
		letter := strings.TrimPrefix(data, "student_class_letter_")
		studentData[chatID].ClassLetter = letter
		studentFSM[chatID] = StateStudentWaitingConfirm

		id, err := SaveStudentRequest(database, chatID, studentData[chatID])
		if err != nil {
			fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
			bot.Send(tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Ошибка при сохранении заявки. Попробуйте позже."))
			delete(studentFSM, chatID)
			delete(studentData, chatID)
			return
		}
		fsmutil.DisableMarkup(bot, chatID, cb.Message.MessageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "Заявка на регистрацию отправлена администратору. Ожидайте подтверждения."))
		handlers.NotifyAdminsAboutNewUser(bot, database, id)
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		return
	}
}
