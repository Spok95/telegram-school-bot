package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type AddFSMState struct {
	Step               int
	ClassNumber        int64
	ClassLetter        string
	SelectedStudentIDs []int64
	CategoryID         int
	LevelID            int
	Comment            string
}

var addStates = make(map[int64]*AddFSMState)

// ==== helpers ====

func addBackCancelRow() []tgbotapi.InlineKeyboardButton {
	row := fsmutil.BackCancelRow("add_back", "add_cancel")
	return row
}

func addEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	bot.Send(cfg)
}

func addClassNumberRows() [][]tgbotapi.InlineKeyboardButton {
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, num := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
		callback := fmt.Sprintf("add_class_num_%d", num)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", num), callback),
		))
	}
	buttons = append(buttons, addBackCancelRow())
	return buttons
}

func addClassLetterRows(prefix string) [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l, prefix+l),
		))
	}
	rows = append(rows, addBackCancelRow())
	return rows
}

// ==== start ====

func StartAddScoreFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	delete(addStates, chatID)
	addStates[chatID] = &AddFSMState{
		Step:               1,
		SelectedStudentIDs: []int64{},
	}

	out := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
	out.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(addClassNumberRows()...)
	bot.Send(out)
}

// ==== callbacks ====

func HandleAddScoreCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.From.ID
	state, ok := addStates[chatID]
	if !ok {
		return
	}
	data := cq.Data

	// ❌ Отмена — прячем клавиатуру у этого сообщения и меняем текст
	if data == "add_cancel" {
		delete(addStates, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Начисление отменено.")
		bot.Send(edit)
		return
	}

	// ⬅ Назад
	if data == "add_back" {
		switch state.Step {
		case 2: // выбирали букву → вернёмся к номеру
			state.Step = 1
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите номер класса:", addClassNumberRows())
			return
		case 3: // выбирали учеников → вернёмся к букве
			state.Step = 2
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", addClassLetterRows("add_class_letter_"))
			return
		case 4: // выбирали категорию → назад к ученикам
			state.Step = 3
			// пересоберём список учеников
			students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
			var buttons [][]tgbotapi.InlineKeyboardButton
			for _, s := range students {
				label := s.Name
				if containsInt64(state.SelectedStudentIDs, s.ID) {
					label = "✅ " + label
				}
				callback := fmt.Sprintf("addscore_student_%d", s.ID)
				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(label, callback),
				))
			}
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Выбрать всех", "add_select_all_students"),
			))
			buttons = append(buttons, addBackCancelRow())
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите ученика или учеников:", buttons)
			return
		case 5: // выбирали уровень → назад к категории
			state.Step = 4
			user, _ := db.GetUserByTelegramID(database, chatID)
			categories, _ := db.GetAllCategories(database, string(*user.Role))
			var buttons [][]tgbotapi.InlineKeyboardButton
			for _, c := range categories {
				callback := fmt.Sprintf("addscore_category_%d", c.ID)
				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(c.Name, callback),
				))
			}
			buttons = append(buttons, addBackCancelRow())
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите категорию:", buttons)
			return
		case 6: // ввод комментария → назад к уровню
			state.Step = 5
			levels, _ := db.GetLevelsByCategoryID(database, state.CategoryID)
			var buttons [][]tgbotapi.InlineKeyboardButton
			for _, l := range levels {
				callback := fmt.Sprintf("addscore_level_%d", l.ID)
				label := fmt.Sprintf("%s (%d)", l.Label, l.Value)
				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(label, callback),
				))
			}
			buttons = append(buttons, addBackCancelRow())
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите уровень:", buttons)
			return
		default:
			delete(addStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Начисление отменено.")
			bot.Send(edit)
			return
		}
	}

	// ==== обычные ветки ====

	if strings.HasPrefix(data, "add_class_num_") {
		numStr := strings.TrimPrefix(data, "add_class_num_")
		num, _ := strconv.ParseInt(numStr, 10, 64)
		state.ClassNumber = num
		state.Step = 2

		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", addClassLetterRows("add_class_letter_"))
		return
	}

	if strings.HasPrefix(data, "add_class_letter_") {
		state.ClassLetter = strings.TrimPrefix(data, "add_class_letter_")
		state.Step = 3

		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
		if len(students) == 0 {
			delete(addStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ В этом классе нет учеников.")
			bot.Send(edit)
			return
		}
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			callback := fmt.Sprintf("addscore_student_%d", s.ID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(s.Name, callback),
			))
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Выбрать всех", "add_select_all_students"),
		))
		buttons = append(buttons, addBackCancelRow())

		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите ученика или учеников:", buttons)
		return
	}

	if strings.HasPrefix(data, "addscore_student_") || data == "add_select_all_students" {
		idStr := strings.TrimPrefix(data, "addscore_student_")
		id, _ := strconv.ParseInt(idStr, 10, 64)

		if data != "add_select_all_students" {
			if !containsInt64(state.SelectedStudentIDs, id) {
				state.SelectedStudentIDs = append(state.SelectedStudentIDs, id)
			}
		} else {
			students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
			for _, s := range students {
				if !containsInt64(state.SelectedStudentIDs, s.ID) {
					state.SelectedStudentIDs = append(state.SelectedStudentIDs, s.ID)
				}
			}
		}

		// пересобираем клавиатуру
		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			label := s.Name
			if containsInt64(state.SelectedStudentIDs, s.ID) {
				label = "✅ " + label
			}
			callback := fmt.Sprintf("addscore_student_%d", s.ID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, callback),
			))
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Выбрать всех", "add_select_all_students"),
		))
		if len(state.SelectedStudentIDs) > 0 {
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "add_students_done"),
			))
		}
		buttons = append(buttons, addBackCancelRow())

		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите ученика или учеников:", buttons)
		return
	}

	if data == "add_students_done" {
		state.Step = 4
		user, _ := db.GetUserByTelegramID(database, chatID)
		categories, _ := db.GetAllCategories(database, string(*user.Role))
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			callback := fmt.Sprintf("addscore_category_%d", c.ID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c.Name, callback),
			))
		}
		buttons = append(buttons, addBackCancelRow())
		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите категорию:", buttons)
		return
	}

	if strings.HasPrefix(data, "addscore_category_") {
		catID, _ := strconv.Atoi(strings.TrimPrefix(data, "addscore_category_"))
		state.CategoryID = catID
		state.Step = 5
		levels, _ := db.GetLevelsByCategoryID(database, catID)
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, l := range levels {
			callback := fmt.Sprintf("addscore_level_%d", l.ID)
			label := fmt.Sprintf("%s (%d)", l.Label, l.Value)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, callback),
			))
		}
		buttons = append(buttons, addBackCancelRow())
		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите уровень:", buttons)
		return
	}

	if strings.HasPrefix(data, "addscore_level_") {
		lvlID, _ := strconv.Atoi(strings.TrimPrefix(data, "addscore_level_"))
		state.LevelID = lvlID
		state.Step = 6

		// запрос комментария (необязателен) с Back/Cancel
		rows := [][]tgbotapi.InlineKeyboardButton{addBackCancelRow()}
		addEditMenu(bot, chatID, cq.Message.MessageID, "Введите комментарий (необязательно, например: за участие):", rows)
		return
	}
}

// ==== текстовый шаг ====

func HandleAddScoreText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state, ok := addStates[chatID]
	if !ok || state.Step != 6 {
		return
	}

	// текстовая отмена
	if fsmutil.IsCancelText(msg.Text) {
		delete(addStates, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Начисление отменено."))
		return
	}

	state.Comment = strings.TrimSpace(msg.Text)

	// one‑shot защита от двойного сабмита
	key := fmt.Sprintf("add:%d", chatID)
	if !fsmutil.SetPending(chatID, key) {
		bot.Send(tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…"))
		return
	}
	defer fsmutil.ClearPending(chatID, key)

	level, _ := db.GetLevelByID(database, state.LevelID)
	user, _ := db.GetUserByTelegramID(database, chatID)
	createdBy := user.ID
	comment := state.Comment

	_ = db.SetActivePeriod(database)
	period, err := db.GetActivePeriod(database)
	if err != nil || period == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось определить активный период."))
		delete(addStates, chatID)
		return
	}

	for _, sid := range state.SelectedStudentIDs {
		score := models.Score{
			StudentID:  sid,
			CategoryID: int64(state.CategoryID),
			Points:     level.Value,
			Type:       "add",
			Comment:    &comment,
			Status:     "pending",
			CreatedBy:  createdBy,
			CreatedAt:  time.Now(),
			PeriodID:   &period.ID,
		}
		_ = db.AddScore(database, score)

		student, err := db.GetUserByID(database, sid)
		if err != nil {
			log.Println("Ошибка получения ученика:", err)
			continue
		}
		NotifyAdminsAboutScoreRequest(bot, database, score, student.Name)
	}
	bot.Send(tgbotapi.NewMessage(chatID, "Заявки на начисление баллов отправлены на подтверждение."))
	delete(addStates, chatID)
}

// доступ из main.go
func GetAddScoreState(chatID int64) *AddFSMState {
	return addStates[chatID]
}
