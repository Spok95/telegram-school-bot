package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/bot/auth"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"strconv"
)

const (
	StateAddChildName        = "add_child_name"
	StateAddChildClassNumber = "add_child_class_number"
	StateAddChildClassLetter = "add_child_class_letter"
	StateAddChildComplete    = "add_child_complete"
)

var addChildFSM = make(map[int64]string)
var addChildData = make(map[int64]*auth.ParentRegisterData)

func StartAddChild(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	addChildFSM[chatID] = StateAddChildName
	addChildData[chatID] = &auth.ParentRegisterData{}
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ФИО ребёнка, которого хотите добавить:"))
}

func HandleAddChildName(chatID int64, msg string, bot *tgbotapi.BotAPI) {
	addChildData[chatID].StudentName = msg
	addChildFSM[chatID] = StateAddChildClassNumber
	auth.SendParentClassNumberButtons(chatID, bot)
}

func HandleAddChildClassNumber(chatID int64, number int, bot *tgbotapi.BotAPI) {
	addChildData[chatID].ClassNumber = number
	addChildFSM[chatID] = StateAddChildClassLetter
	auth.SendParentClassLetterButtons(chatID, bot)
}

func HandleAddChildClassLetter(chatID int64, letter string, bot *tgbotapi.BotAPI, database *sql.DB) {
	addChildData[chatID].ClassLetter = letter

	studentID, err := auth.FindStudentID(database, addChildData[chatID])
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ученик не найден. Повторите ввод ФИО."))
		addChildFSM[chatID] = StateAddChildName
		return
	}

	// Только добавление связи, без вставки родителя
	err = AddStudentLinkIfParentExists(database, chatID, studentID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при добавлении связи. Попробуйте позже."))
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Ребёнок успешно добавлен!"))
	}

	delete(addChildFSM, chatID)
	delete(addChildData, chatID)
}

func AddStudentLinkIfParentExists(database *sql.DB, telegramID int64, studentID int) error {
	var parentID int
	err := database.QueryRow(`SELECT id FROM users WHERE telegram_id = ? AND role = 'parent' AND confirmed = 1`, telegramID).Scan(&parentID)
	if err != nil {
		return fmt.Errorf("родитель не найден: %w", err)
	}
	_, err = database.Exec(`INSERT INTO parents_students (parent_id, student_id) VALUES (?, ?)`, parentID, studentID)
	if err != nil {
		return fmt.Errorf("ошибка добавления связи: %w", err)
	}
	return nil
}

func HandleAddChildText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := GetAddChildFSMState(chatID)
	switch state {
	case StateAddChildName:
		HandleAddChildName(chatID, msg.Text, bot)
	case StateAddChildClassNumber:
		number, err := strconv.Atoi(msg.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите корректный номер класса (цифрой)"))
			return
		}
		HandleAddChildClassNumber(chatID, number, bot)
	case StateAddChildClassLetter:
		HandleAddChildClassLetter(chatID, msg.Text, bot, database)
	}
}

func GetAddChildFSMState(chatID int64) string {
	return addChildFSM[chatID]
}
