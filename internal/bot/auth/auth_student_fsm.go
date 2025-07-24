package auth

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"strings"
)

type StudentFSMState string

const (
	StateStudentName           StudentFSMState = "student_name"
	StateStudentClassNum       StudentFSMState = "student_class_num"
	StateStudentClassLet       StudentFSMState = "student_class_letter"
	StateStudentWaitingConfirm StudentFSMState = "student_waiting"
)

var studentFSM = make(map[int64]StudentFSMState)
var studentData = make(map[int64]*StudentRegisterData)

type StudentRegisterData struct {
	Name        string
	ClassNumber int
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
		bot.Send(tgbotapi.NewMessage(chatID, "Введите номер класса (например, 7):"))
	case StateStudentClassNum:
		var num int
		_, err := fmt.Sscanf(msg, "%d", &num)
		if err != nil || num < 1 || num > 11 {
			bot.Send(tgbotapi.NewMessage(chatID, "Введите корректный номер класса (1–11):"))
			return
		}
		studentData[chatID].ClassNumber = num
		studentFSM[chatID] = StateStudentClassLet
		bot.Send(tgbotapi.NewMessage(chatID, "Введите букву класса (например, А):"))
	case StateStudentClassLet:
		letter := strings.TrimSpace(msg)
		if len([]rune(letter)) != 1 || !isCyrillicLetter(letter) {
			bot.Send(tgbotapi.NewMessage(chatID, "Введите одну букву класса (только кириллица):"))
			return
		}
		letter = strings.ToUpper(letter)
		studentData[chatID].ClassLetter = letter
		studentFSM[chatID] = StateStudentWaitingConfirm

		// Сохраняем в БД заявку, статус: unconfirmed
		err := SaveStudentRequest(database, chatID, studentData[chatID])
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка при сохранении заявки. Попробуйте позже."))
			return
		}

		bot.Send(tgbotapi.NewMessage(chatID, "Заявка на регистрацию отправлена администратору. Ожидайте подтверждения."))

		handlers.ShowPendingUsers(database, bot)

		delete(studentFSM, chatID)
		delete(studentData, chatID)
	}
}

func SaveStudentRequest(database *sql.DB, chatID int64, data *StudentRegisterData) error {
	_, err := database.Exec(`INSERT INTO users (telegram_id, name, role, class_number, class_letter, confirmed) 
			VALUES (?, ?, 'student', ?, ?, 0)`,
		chatID, data.Name, data.ClassNumber, data.ClassLetter)
	return err
}

func isCyrillicLetter(letter string) bool {
	r := []rune(letter)
	if len(r) != 1 {
		return false
	}
	return r[0] >= 'А' && r[0] <= 'Я'
}
