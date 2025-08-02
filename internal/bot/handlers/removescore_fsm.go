package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strconv"
	"strings"
	"time"
)

type RemoveFSMState struct {
	Step               int
	ClassNumber        int64
	ClassLetter        string
	SelectedStudentIDs []int64
	CategoryID         int
	LevelID            int
	Comment            string
}

var removeStates = make(map[int64]*RemoveFSMState)

func StartRemoveScoreFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	removeStates[chatID] = &RemoveFSMState{Step: 1}

	number := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, num := range number {
		callback := fmt.Sprintf("remove_class_num_%d", num)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", num), callback)))
	}
	msgOut := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
	msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	bot.Send(msgOut)
}

func HandleRemoveCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.From.ID
	state, ok := removeStates[chatID]
	if !ok {
		return
	}

	data := cq.Data
	if strings.HasPrefix(data, "remove_class_num_") {
		numStr := strings.TrimPrefix(data, "remove_class_num_")
		num, _ := strconv.ParseInt(numStr, 10, 64)
		state.ClassNumber = num
		state.Step = 2

		letters := []string{"А", "Б", "В", "Г", "Д"}
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, l := range letters {
			callback := fmt.Sprintf("remove_class_letter_%s", l)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(l, callback)))
		}
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите букву класса:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
		bot.Send(edit)
	} else if strings.HasPrefix(data, "remove_class_letter_") {
		state.ClassLetter = strings.TrimPrefix(data, "remove_class_letter_")
		state.Step = 3

		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
		if len(students) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ В этом классе нет учеников."))
			delete(removeStates, chatID)
			return
		}
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			callback := fmt.Sprintf("remove_student_%d", s.ID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(s.Name, callback)))
		}
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите ученика или учеников:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
		bot.Send(edit)
	} else if strings.HasPrefix(data, "remove_student_") {
		idStr := strings.TrimPrefix(data, "remove_student_")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		// Если ученик не выбран — добавляем
		if !containsInt64(state.SelectedStudentIDs, id) {
			state.SelectedStudentIDs = append(state.SelectedStudentIDs, id)
		}
		// Получаем учеников и пересобираем клавиатуру
		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			label := s.Name
			callback := fmt.Sprintf("remove_student_%d", s.ID)

			// Отметим выбранного
			if containsInt64(state.SelectedStudentIDs, s.ID) {
				label = "✅ " + label
			}
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, callback)))
		}

		// Показываем кнопку "Готово" только если выбран хотя бы один
		if len(state.SelectedStudentIDs) > 0 {
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "remove_students_done"),
			))
		}

		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите ученика или учеников:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
		bot.Send(edit)
	} else if data == "remove_students_done" {
		state.Step = 4
		user, _ := db.GetUserByTelegramID(database, chatID)
		categories, _ := db.GetAllCategories(database, string(*user.Role))
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			callback := fmt.Sprintf("remove_category_%d", c.ID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(c.Name, callback)))
		}
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите категорию:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
		bot.Send(edit)
	} else if strings.HasPrefix(data, "remove_category_") {
		catID, _ := strconv.Atoi(strings.TrimPrefix(data, "remove_category_"))
		state.CategoryID = catID
		state.Step = 5
		levels, _ := db.GetLevelsByCategoryID(database, catID)
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, l := range levels {
			callback := fmt.Sprintf("remove_level_%d", l.ID)
			label := fmt.Sprintf("%s (%d)", l.Label, l.Value)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, callback)))
		}
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите уровень:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
		bot.Send(edit)
	} else if strings.HasPrefix(data, "remove_level_") {
		lvlID, _ := strconv.Atoi(strings.TrimPrefix(data, "remove_level_"))
		state.LevelID = lvlID
		state.Step = 6
		msg := tgbotapi.NewMessage(chatID, "Введите комментарий (обязателен для списания):")
		bot.Send(msg)
	}
}

func HandleRemoveText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state, ok := removeStates[chatID]
	if !ok || state.Step != 6 {
		return
	}

	state.Comment = msg.Text
	level, _ := db.GetLevelByID(database, state.LevelID)
	user, _ := db.GetUserByTelegramID(database, chatID)
	createdBy := user.ID
	comment := state.Comment

	_ = db.SetActivePeriod(database)
	period, err := db.GetActivePeriod(database)
	if err != nil || period == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось определить активный период."))
		return
	}

	for _, sid := range state.SelectedStudentIDs {
		score := models.Score{
			StudentID:  sid,
			CategoryID: int64(state.CategoryID),
			Points:     level.Value,
			Type:       "remove",
			Comment:    &comment,
			Status:     "pending",
			CreatedBy:  createdBy,
			CreatedAt:  time.Now(),
			PeriodID:   &period.ID,
		}
		db.AddScore(database, score)
		student, err := db.GetUserByID(database, sid)
		if err != nil {
			log.Println("Ошибка получения ученика:", err)
			return
		}
		studentName := student.Name
		NotifyAdminsAboutScoreRequest(bot, database, score, studentName)
	}
	bot.Send(tgbotapi.NewMessage(chatID, "Заявки на списание баллов отправлены на подтверждение."))

	delete(removeStates, chatID)
}

func containsInt64(slice []int64, item int64) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func GetRemoveScoreState(chatID int64) *RemoveFSMState {
	return removeStates[chatID]
}
