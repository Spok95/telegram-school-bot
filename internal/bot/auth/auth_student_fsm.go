package auth

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"strconv"
	"strings"
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

// Начало FSM ученика
func StartStudentRegistration(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	delete(studentFSM, chatID)
	delete(studentData, chatID)

	studentFSM[chatID] = StateStudentName
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ваше ФИО:"))
}

// Обработка шагов FSM
func HandleStudentFSM(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	state := studentFSM[chatID]

	switch state {
	case StateStudentName:
		studentData[chatID] = &StudentRegisterData{Name: msg}
		studentFSM[chatID] = StateStudentClassNum
		showClassNumberButtons(chatID, bot)
	}
}

func SaveStudentRequest(database *sql.DB, chatID int64, data *StudentRegisterData) error {
	classID, err := db.ClassIDByNumberAndLetter(database, data.ClassNumber, data.ClassLetter)
	if err != nil {
		return fmt.Errorf("❌ Ошибка: выбранный класс не существует. Обратитесь к администратору.%w", err)
	}
	_, err = database.Exec(`INSERT INTO users (telegram_id, name, role, class_id, class_number, class_letter, confirmed) 
			VALUES (?, ?, 'student', ?, ?, ?, 0)`,
		chatID, data.Name, classID, data.ClassNumber, data.ClassLetter)
	return err
}

func HandleStudentCallback(cb *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB) {
	chatID := cb.Message.Chat.ID
	data := cb.Data

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
		showClassLetterButtons(chatID, bot)
		return
	}

	if strings.HasPrefix(data, "student_class_letter_") {
		letter := strings.TrimPrefix(data, "student_class_letter_")
		studentData[chatID].ClassLetter = letter
		studentFSM[chatID] = StateStudentWaitingConfirm

		err := SaveStudentRequest(database, chatID, studentData[chatID])
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка при сохранении заявки. Попробуйте позже."))
			delete(studentFSM, chatID)
			delete(studentData, chatID)
			return
		}

		bot.Send(tgbotapi.NewMessage(chatID, "Заявка на регистрацию отправлена администратору. Ожидайте подтверждения."))

		handlers.ShowPendingUsers(database, bot)
		delete(studentFSM, chatID)
		delete(studentData, chatID)
		return
	}
}

func showClassNumberButtons(chatID int64, bot *tgbotapi.BotAPI) {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		btn := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", i), fmt.Sprintf("student_class_num_%d", i))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	msg := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msg)
}

func showClassLetterButtons(chatID int64, bot *tgbotapi.BotAPI) {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		btn := tgbotapi.NewInlineKeyboardButtonData(l, fmt.Sprintf("student_class_letter_%s", l))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	msg := tgbotapi.NewMessage(chatID, "Выберите букву класса:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msg)
}
