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
	if state == nil {
		return
	}

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
		} else if state.Mode == "class" {
			students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
			for _, s := range students {
				state.SelectedStudentIDs = append(state.SelectedStudentIDs, s.ID)
			}
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
		total, err := db.GetApprovedScoreSum(database, studentID)
		if err != nil {
			log.Println("❌ Ошибка при получении баллов:", err)
			continue
		}
		if total < points {
			student, _ := db.GetUserByID(database, studentID)
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

	comment := "Аукцион"
	for _, studentID := range state.SelectedStudentIDs {
		score := models.Score{
			StudentID:  studentID,
			CategoryID: 999,
			Points:     points,
			Type:       "remove",
			Comment:    &comment,
			Status:     "pending",
			CreatedBy:  user.ID,
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
