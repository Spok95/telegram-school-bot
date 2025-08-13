package auth

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

func parentBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("parent_back", "parent_cancel")
}

func parentClassNumberRows() [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		cb := fmt.Sprintf("parent_class_num_%d", i)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", i), cb),
		))
	}
	rows = append(rows, parentBackCancelRow())
	return rows
}

func parentClassLetterRows() [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l, "parent_class_letter_"+l),
		))
	}
	rows = append(rows, parentBackCancelRow())
	return rows
}

func parentEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	bot.Send(cfg)
}

func StartParentRegistration(chatID int64, user *tgbotapi.User, bot *tgbotapi.BotAPI, database *sql.DB) {
	parentFSM[chatID] = StateParentStudentName
	parentName := strings.TrimSpace(fmt.Sprintf("%s %s", user.FirstName, user.LastName))
	parentData[chatID] = &ParentRegisterData{ParentName: parentName}
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ФИО ребёнка, которого вы представляете:"))
}

func HandleParentFSM(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	trimmed := strings.TrimSpace(msg)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново."))
		return
	}

	state := parentFSM[chatID]

	switch state {
	case StateParentStudentName:
		if parentData[chatID] == nil {
			parentData[chatID] = &ParentRegisterData{}
		}
		parentData[chatID].StudentName = msg
		parentFSM[chatID] = StateParentClassNumber
		msgOut := tgbotapi.NewMessage(chatID, "Выберите номер класса ребёнка:")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(parentClassNumberRows()...)
		bot.Send(msgOut)
	}
}

func HandleParentCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	data := cq.Data
	state := parentFSM[chatID]

	if data == "parent_cancel" {
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново."))
		return
	}
	if data == "parent_back" {
		switch state {
		case StateParentClassNumber:
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			bot.Send(tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Введите ФИО ребёнка:"))
			parentFSM[chatID] = StateParentStudentName
		case StateParentClassLetter:
			parentFSM[chatID] = StateParentClassNumber
			parentEditMenu(bot, chatID, cq.Message.MessageID, "Выберите номер класса ребёнка:", parentClassNumberRows())
		case StateParentWaiting:
			bot.Request(tgbotapi.NewCallback(cq.ID, "Заявка уже отправлена, ожидайте подтверждения."))
		default:
			bot.Request(tgbotapi.NewCallback(cq.ID, "Действие недоступно на этом шаге."))
		}
		return
	}

	if strings.HasPrefix(data, "parent_class_num_") {
		numStr := strings.TrimPrefix(data, "parent_class_num_")
		num, _ := strconv.Atoi(numStr)
		if parentData[chatID] == nil {
			parentData[chatID] = &ParentRegisterData{}
		}
		parentData[chatID].ClassNumber = num
		parentFSM[chatID] = StateParentClassLetter
		parentEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", parentClassLetterRows())
		return
	}

	if strings.HasPrefix(data, "parent_class_letter_") {
		letter := strings.TrimPrefix(data, "parent_class_letter_")
		parentData[chatID].ClassLetter = letter
		parentFSM[chatID] = StateParentWaiting

		studentID, err := FindStudentID(database, parentData[chatID])

		if err != nil {
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			bot.Send(tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ Ученик не найден. Введите ФИО заново:"))
			parentFSM[chatID] = StateParentStudentName
			return
		}

		parentID, err := SaveParentRequest(database, chatID, studentID, parentData[chatID].ParentName)
		if err != nil {
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			bot.Send(tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Ошибка при сохранении. Попробуйте позже."))
			delete(parentFSM, chatID)
			delete(parentData, chatID)
			return
		}
		handlers.NotifyAdminsAboutNewUser(bot, database, parentID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Заявка на регистрацию родителя отправлена администратору. Ожидайте подтверждения."))
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		return
	}
}

func FindStudentID(database *sql.DB, data *ParentRegisterData) (int, error) {
	var id int
	err := database.QueryRow(`
		SELECT id FROM users
		WHERE name = $1 AND class_number = $2 AND class_letter = $3 AND role = 'student' AND confirmed = 1
	`, data.StudentName, data.ClassNumber, data.ClassLetter).Scan(&id)
	return id, err
}

func SaveParentRequest(database *sql.DB, parentTelegramID int64, studentID int, parentName string) (int64, error) {
	var parentID int64
	tx, err := database.Begin()
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to begin transaction: %v", err)
		return 0, err
	}
	err = tx.QueryRow(`SELECT id FROM users WHERE telegram_id = $1`, parentTelegramID).Scan(&parentID)
	if err == sql.ErrNoRows {
		// Вставка родителя в users
		res, err := tx.Exec(`
		INSERT INTO users (telegram_id, name, role, confirmed)
		VALUES ($1, $2, 'parent', 0)
	`, parentTelegramID, parentName)
		if err != nil {
			log.Printf("[PARENT_ERROR] failed to insert parent user: %v", err)
			tx.Rollback()
			return 0, err
		}
		parentID, _ = res.LastInsertId()
	} else if err != nil {
		tx.Rollback()
		return 0, err
	}

	// Привязка к ученику
	_, err = tx.Exec(`INSERT INTO parents_students (parent_id, student_id) VALUES ($1, $2)`, parentID, studentID)
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to insert into parents_students: %v", err)
		tx.Rollback()
		return 0, err
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to commit transaction: %v", err)
		return 0, err
	}

	log.Printf("[PARENT_SUCCESS] linked parent (tg_id=%d) to student_id=%d", parentTelegramID, studentID)
	return parentID, nil
}
