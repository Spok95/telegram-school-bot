package auth

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
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

func StartParentRegistration(chatID int64, msg string, bot *tgbotapi.BotAPI, database *sql.DB) {
	parentFSM[chatID] = StateParentStudentName
	parentData[chatID] = &ParentRegisterData{ParentName: msg}
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
		bot.Send(tgbotapi.NewMessage(chatID, "Введите номер класса ребёнка:"))
	case StateParentClassNumber:
		var num int
		_, err := fmt.Sscanf(msg, "%d", &num)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Введите корректный номер класса."))
			return
		}
		parentData[chatID].ClassNumber = num
		parentFSM[chatID] = StateParentClassLetter
		bot.Send(tgbotapi.NewMessage(chatID, "Введите букву класса ребёнка:"))
	case StateParentClassLetter:
		parentData[chatID].ClassLetter = msg
		parentFSM[chatID] = StateParentWaiting

		// Проверяем ученика
		studentID, err := FindStudentID(database, parentData[chatID])
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ученик не найден. Проверьте данные и введите снова ФИО."))
			parentFSM[chatID] = StateParentStudentName
			return
		}

		log.Printf("[PARENT_REG] chatID=%d | StudentName='%s' | ClassNumber=%d | ClassLetter='%s' | ParentName='%s'\n",
			chatID,
			parentData[chatID].StudentName,
			parentData[chatID].ClassNumber,
			parentData[chatID].ClassLetter,
			parentData[chatID].ParentName,
		)

		// Сохраняем родителя в users, добавляем запись в parents_students
		err = SaveParentRequest(database, chatID, studentID, parentData[chatID].ParentName)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка при сохранении заявки. Попробуйте позже."))
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "Заявка на регистрацию родителя отправлена администратору. Ожидайте подтверждения."))

		handlers.ShowPendingUsers(database, bot)

		delete(studentFSM, chatID)
		delete(studentData, chatID)
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
