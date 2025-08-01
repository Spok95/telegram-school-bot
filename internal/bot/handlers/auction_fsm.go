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

const (
	AuctionStepMode = iota
	AuctionStepClassNumber
	AuctionStepClassLetter
	AuctionStepStudentSelect
	AuctionStepPoints
)

type AuctionFSMState struct {
	Step               int
	Mode               string // "students" or "class"
	ClassNumber        int64
	ClassLetter        string
	SelectedStudentIDs []int64
	PointsToRemove     int
}

var auctionStates = make(map[int64]*AuctionFSMState)

func StartAuctionFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	auctionStates[chatID] = &AuctionFSMState{Step: AuctionStepMode}

	text := "Выберите режим аукциона:\n🧍 Ученики — списать с отдельных учеников\n🏫 Класс — списать со всего класса"
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧍 Ученики", "auction_mode_students"),
			tgbotapi.NewInlineKeyboardButtonData("🏫 Класс", "auction_mode_class"),
		),
	)
	msgOut := tgbotapi.NewMessage(chatID, text)
	msgOut.ReplyMarkup = markup
	bot.Send(msgOut)
}

func HandleAuctionCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state := auctionStates[chatID]

	data := cq.Data
	log.Printf("➡️ Callback от аукциона: %s", data)

	switch {
	case strings.HasPrefix(data, "auction_mode_"):
		mode := strings.TrimPrefix(data, "auction_mode_")
		state.Mode = mode
		state.Step = AuctionStepClassNumber
		promptClassNumber(cq, bot)

	case strings.HasPrefix(data, "auction_class_number_"):
		numStr := strings.TrimPrefix(data, "auction_class_number_")
		classNumber, _ := strconv.ParseInt(numStr, 10, 64)
		state.ClassNumber = classNumber
		state.Step = AuctionStepClassLetter
		promptClassLetter(cq, bot)

	case strings.HasPrefix(data, "auction_class_letter_"):
		letter := strings.TrimPrefix(data, "auction_class_letter_")
		state.ClassLetter = letter
		if state.Mode == "students" {
			state.Step = AuctionStepStudentSelect
			promptStudentSelect(cq, bot, database)
		} else {
			state.Step = AuctionStepPoints
			promptPointsInput(cq.Message, bot)
		}

	case strings.HasPrefix(data, "auction_select_student_"):
		idStr := strings.TrimPrefix(data, "auction_select_student_")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		found := false
		for i, existing := range state.SelectedStudentIDs {
			if existing == id {
				state.SelectedStudentIDs = append(state.SelectedStudentIDs[:i], state.SelectedStudentIDs[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			state.SelectedStudentIDs = append(state.SelectedStudentIDs, id)
		}
		promptStudentSelect(cq, bot, database)

	case data == "auction_students_done":
		if len(state.SelectedStudentIDs) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Выберите хотя бы одного ученика."))
			return
		}
		state.Step = AuctionStepPoints
		promptPointsInput(cq.Message, bot)
	}
}

func HandleAuctionText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := auctionStates[chatID]

	if state == nil || state.Step != AuctionStepPoints {
		return
	}

	points, err := strconv.Atoi(msg.Text)
	if err != nil || points <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите корректное положительное число."))
		return
	}

	state.PointsToRemove = points
	notEnough := []string{}
	for _, studentID := range state.SelectedStudentIDs {
		student, err := db.GetUserByID(database, studentID)
		if err != nil {
			log.Println("❌ Ошибка при получении ученика:", err)
			continue
		}

		total, err := db.GetApprovedScoreSum(database, studentID)
		if err != nil {
			log.Println("❌ Ошибка при получении баллов:", err)
			continue
		}

		if total < points {
			notEnough = append(notEnough, student.Name)
		}
	}

	if len(notEnough) > 0 {
		text := "❌ У следующих учеников недостаточно баллов:\n" + strings.Join(notEnough, "\n")
		bot.Send(tgbotapi.NewMessage(chatID, text))
		delete(auctionStates, chatID)
		return
	}
	user, err := db.GetUserByTelegramID(database, chatID)
	if err != nil {
		log.Println("❌ Ошибка получения пользователя:", err)
		return
	}
	period, err := db.GetActivePeriod(database)
	if err != nil || period == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось определить активный период."))
		return
	}
	createdBy := user.ID

	comment := "Аукцион"
	for _, studentID := range state.SelectedStudentIDs {
		score := models.Score{
			StudentID:  studentID,
			CategoryID: 999,
			Points:     points,
			Type:       "remove",
			Comment:    &comment,
			Status:     "pending",
			CreatedBy:  createdBy,
			CreatedAt:  time.Now(),
			PeriodID:   &period.ID,
		}
		_ = db.AddScore(database, score)

		student, _ := db.GetUserByID(database, studentID)
		NotifyAdminsAboutScoreRequest(bot, database, score, student.Name)
	}

	bot.Send(tgbotapi.NewMessage(chatID, "✅ Заявка на аукцион создана и ожидает подтверждения."))
	delete(auctionStates, chatID)
}

func promptClassNumber(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(cq.Message.Chat.ID, "🔢 Выберите номер класса:")
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for i := 1; i <= 11; i++ {
		btn := tgbotapi.NewInlineKeyboardButtonData(strconv.Itoa(i), fmt.Sprintf("auction_class_number_%d", i))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msg)
}

func promptClassLetter(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(cq.Message.Chat.ID, "🔠 Выберите букву класса:")
	letters := []string{"А", "Б", "В", "Г", "Д"}
	row := []tgbotapi.InlineKeyboardButton{}
	for _, l := range letters {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(l, "auction_class_letter_"+l))
	}
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
	bot.Send(msg)
}

func promptStudentSelect(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB) {
	chatID := cq.Message.Chat.ID
	state := auctionStates[chatID]
	students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, student := range students {
		selected := ""
		for _, id := range state.SelectedStudentIDs {
			if id == student.ID {
				selected = " ✅"
				break
			}
		}
		label := fmt.Sprintf("%s%s", student.Name, selected)
		cbData := fmt.Sprintf("auction_select_student_%d", student.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, cbData)))
	}
	if len(state.SelectedStudentIDs) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Готово", "auction_students_done")))
	}

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "👥 Выберите учеников для аукциона:", tgbotapi.NewInlineKeyboardMarkup(rows...))
	bot.Send(edit)
}

func promptPointsInput(msg *tgbotapi.Message, bot *tgbotapi.BotAPI) {
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✏️ Введите количество баллов для списания:"))
}

func GetAuctionState(userID int64) *AuctionFSMState {
	return auctionStates[userID]
}

//func HandleAuctionCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
//	chatID := cq.From.ID
//	state, ok := auctionStates[chatID]
//	if !ok {
//		return
//	}
//	data := cq.Data
//	switch {
//	case strings.HasPrefix(data, "auction_mode_"):
//		mode := strings.TrimPrefix(data, "auction_mode_")
//		state.Mode = mode
//		state.Step = AuctionStepClassNumber
//		number := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
//		var buttons [][]tgbotapi.InlineKeyboardButton
//		for _, num := range number {
//			callback := fmt.Sprintf("auction_class_num_%d", num)
//			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", num), callback)))
//		}
//		msgOut := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
//		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
//		bot.Send(msgOut)
//
//	case strings.HasPrefix(data, "auction_class_num_"):
//		numStr := strings.TrimPrefix(data, "auction_class_num_")
//		num, _ := strconv.ParseInt(numStr, 10, 64)
//		state.ClassNumber = num
//		state.Step = AuctionStepClassLetter
//		letters := []string{"А", "Б", "В", "Г", "Д"}
//		var buttons [][]tgbotapi.InlineKeyboardButton
//		for _, l := range letters {
//			callback := fmt.Sprintf("auction_class_letter_%s", l)
//			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(l, callback)))
//		}
//		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите букву класса:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
//		bot.Send(edit)
//
//	case strings.HasPrefix(data, "auction_class_letter_"):
//		state.ClassLetter = strings.TrimPrefix(data, "auction_class_letter_")
//		state.Step = AuctionStepStudentSelect
//		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
//		if len(students) == 0 {
//			bot.Send(tgbotapi.NewMessage(chatID, "❌ В этом классе нет учеников."))
//			delete(auctionStates, chatID)
//			return
//		}
//		var buttons [][]tgbotapi.InlineKeyboardButton
//		for _, s := range students {
//			callback := fmt.Sprintf("auction_student_%d", s.ID)
//			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(s.Name, callback)))
//		}
//		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите ученика или учеников:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
//		bot.Send(edit)
//	case strings.HasPrefix(data, "auction_student_"):
//		idStr := strings.TrimPrefix(data, "auction_student_")
//		id, _ := strconv.ParseInt(idStr, 10, 64)
//
//		// Если ученик не выбран — добавляем
//		if !containsInt64(state.SelectedStudentIDs, id) {
//			state.SelectedStudentIDs = append(state.SelectedStudentIDs, id)
//		}
//		// Получаем учеников и пересобираем клавиатуру
//		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
//		var buttons [][]tgbotapi.InlineKeyboardButton
//		for _, s := range students {
//			label := s.Name
//			callback := fmt.Sprintf("auction_student_%d", s.ID)
//
//			// Отметим выбранного
//			if containsInt64(state.SelectedStudentIDs, s.ID) {
//				label = "✅ " + label
//			}
//			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, callback)))
//		}
//
//		// Показываем кнопку "Готово" только если выбран хотя бы один
//		if len(state.SelectedStudentIDs) > 0 {
//			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
//				tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "auction_done"),
//			))
//		}
//
//		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "Выберите ученика или учеников:", tgbotapi.NewInlineKeyboardMarkup(buttons...))
//		bot.Send(edit)
//	case strings.HasPrefix(data, "auction_done"):
//		state.Step = AuctionStepInputPoints
//		msg := tgbotapi.NewMessage(chatID, "Введите количество баллов для списания у выбранных учеников:")
//		bot.Send(msg)
//	}
//}
//
//func HandleAuctionPointsInput(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
//	chatID := msg.Chat.ID
//	state, ok := auctionStates[chatID]
//	if !ok || state.Step != AuctionStepInputPoints {
//		return
//	}
//
//	points, err := strconv.Atoi(msg.Text)
//	if err != nil || points <= 0 {
//		bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите корректное число баллов."))
//		return
//	}
//	state.PointsToRemove = points
//
//	var insufficient []string
//	for _, studentID := range state.SelectedStudentIDs {
//		balance, err := db.GetTotalApprovedPointsByStudent(database, studentID)
//		if err != nil || balance < points {
//			user, _ := db.GetUserByID(database, studentID)
//			insufficient = append(insufficient, user.Name)
//		}
//	}
//	if len(insufficient) > 0 {
//		text := "❌ У следующих учеников недостаточно баллов:\n"
//		for _, name := range insufficient {
//			text += "• " + name + "\n"
//		}
//		bot.Send(tgbotapi.NewMessage(chatID, text))
//		delete(auctionStates, chatID)
//		return
//	}
//	comment := "Аукцион"
//	for _, studentID := range state.SelectedStudentIDs {
//		_ = db.AddScore(database, models.Score{
//			StudentID: studentID,
//			Points:    points,
//			Type:      "remove",
//			Comment:   &comment,
//			Status:    "pending",
//			CreatedBy: chatID,
//		})
//	}
//	bot.Send(tgbotapi.NewMessage(chatID, "📦 Заявки успешно созданы и ожидают подтверждения."))
//	delete(auctionStates, chatID)
//}
