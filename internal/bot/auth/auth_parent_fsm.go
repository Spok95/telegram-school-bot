package auth

import (
	"database/sql"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strings"
)

type ParentFSMState string

const (
	StateParentStudentName ParentFSMState = "parent_student_name"
	StateParentClassNumber ParentFSMState = "parent_class_number"
	StateParentClassLetter ParentFSMState = "parent_class_letter"
	StateParentWaiting     ParentFSMState = "parent_waiting"
)

var parentFSM = make(map[int64]ParentFSMState)
var parentData = make(map[int64]*ParentRegisterData)

type ParentRegisterData struct {
	StudentName string
	ClassNumber int
	ClassLetter string
	ParentName  string
}

func StartParentRegistration(chatID int64, user *tgbotapi.User, bot *tgbotapi.BotAPI, database *sql.DB) {
	parentFSM[chatID] = StateParentStudentName
	parentName := strings.TrimSpace(fmt.Sprintf("%s %s", user.FirstName, user.LastName))
	parentData[chatID] = &ParentRegisterData{ParentName: parentName}
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ФИО ребёнка, которого вы представляете:"))
}

func HandleParentFSM(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	state := parentFSM[chatID]

	switch state {
	case StateParentStudentName:
		if parentData[chatID] == nil {
			parentData[chatID] = &ParentRegisterData{}
		}
		parentData[chatID].StudentName = msg
		parentFSM[chatID] = StateParentClassNumber
		SendParentClassNumberButtons(chatID, bot)
	}
}

func FindStudentID(database *sql.DB, data *ParentRegisterData) (int, error) {
	var id int
	err := database.QueryRow(`
		SELECT id FROM users
		WHERE name = ? AND class_number = ? AND class_letter = ? AND role = 'student' AND confirmed = 1
	`, data.StudentName, data.ClassNumber, data.ClassLetter).Scan(&id)
	return id, err
}

func SaveParentRequest(database *sql.DB, parentTelegramID int64, studentID int, parentName string) error {
	var parentID int64
	tx, err := database.Begin()
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to begin transaction: %v", err)
		return err
	}
	err = tx.QueryRow(`SELECT id FROM users WHERE telegram_id = ?`, parentTelegramID).Scan(&parentID)
	if err == sql.ErrNoRows {
		// Вставка родителя в users
		res, err := tx.Exec(`
		INSERT INTO users (telegram_id, name, role, confirmed)
		VALUES (?, ?, 'parent', 0)
	`, parentTelegramID, parentName)
		if err != nil {
			log.Printf("[PARENT_ERROR] failed to insert parent user: %v", err)
			tx.Rollback()
			return err
		}
		parentID, _ = res.LastInsertId()
	} else if err != nil {
		tx.Rollback()
		return err
	}

	// Привязка к ученику
	_, err = tx.Exec(`INSERT INTO parents_students (parent_id, student_id) VALUES (?, ?)`, parentID, studentID)
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to insert into parents_students: %v", err)
		tx.Rollback()
		return err
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to commit transaction: %v", err)
		return err
	}

	log.Printf("[PARENT_SUCCESS] linked parent (tg_id=%d) to student_id=%d", parentTelegramID, studentID)
	return nil
}

func HandleParentClassNumber(chatID int64, num int, bot *tgbotapi.BotAPI) {
	if parentData[chatID] == nil {
		parentData[chatID] = &ParentRegisterData{}
	}
	parentData[chatID].ClassNumber = num
	parentFSM[chatID] = StateParentClassLetter
	SendParentClassLetterButtons(chatID, bot)
}

func HandleParentClassLetter(chatID int64, letter string, bot *tgbotapi.BotAPI, database *sql.DB) {
	if parentData[chatID] == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Произошла ошибка. Начните регистрацию заново."))
		return
	}
	parentData[chatID].ClassLetter = letter
	parentFSM[chatID] = StateParentWaiting

	// Проверка ученика
	studentID, err := FindStudentID(database, parentData[chatID])
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ученик не найден. Введите ФИО заново:"))
		parentFSM[chatID] = StateParentStudentName
		return
	}

	err = SaveParentRequest(database, chatID, studentID, parentData[chatID].ParentName)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Ошибка при сохранении. Попробуйте позже."))
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		return
	}
	bot.Send(tgbotapi.NewMessage(chatID, "Заявка на регистрацию родителя отправлена администратору. Ожидайте подтверждения."))

	delete(parentFSM, chatID)
	delete(parentData, chatID)
}

func SendParentClassNumberButtons(chatID int64, bot *tgbotapi.BotAPI) {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		text := fmt.Sprintf("%d класс", i)
		data := fmt.Sprintf("parent_class_num_%d", i)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(text, data)))
	}
	msg := tgbotapi.NewMessage(chatID, "Выберите номер класса ребёнка:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msg)
}

func SendParentClassLetterButtons(chatID int64, bot *tgbotapi.BotAPI) {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(l, "parent_class_letter_"+l)))
	}
	msg := tgbotapi.NewMessage(chatID, "Выберите букву класса:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msg)
}
