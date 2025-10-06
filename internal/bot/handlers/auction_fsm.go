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
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

// ——— helpers ———

func auctionBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("auction_back", "auction_cancel")
}

// ——— start ———

func StartAuctionFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, _ := db.GetUserByTelegramID(database, chatID)
	if u == nil || !fsmutil.MustBeActiveForOps(u) {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Доступ временно закрыт. Обратитесь к администратору.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	auctionStates[chatID] = &AuctionFSMState{Step: AuctionStepMode}

	text := "Выберите режим аукциона:\n🧍 Ученики — списать с отдельных учеников\n🏫 Класс — списать со всего класса"
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧍 Ученики", "auction_mode_students"),
			tgbotapi.NewInlineKeyboardButtonData("🏫 Класс", "auction_mode_class"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "auction_cancel"),
		),
	)
	msgOut := tgbotapi.NewMessage(chatID, text)
	msgOut.ReplyMarkup = markup
	if _, err := tg.Send(bot, msgOut); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// ——— callbacks ———

func HandleAuctionCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state := auctionStates[chatID]
	if state == nil {
		return
	}

	data := cq.Data

	// ❌ Отмена
	if data == "auction_cancel" {
		delete(auctionStates, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Аукцион отменён.")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	// ⬅ Назад
	if data == "auction_back" {
		switch state.Step {
		case AuctionStepClassNumber: // назад к режиму
			state.Step = AuctionStepMode
			text := "Выберите режим аукциона:\n🧍 Ученики — списать с отдельных учеников\n🏫 Класс — списать со всего класса"
			markup := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🧍 Ученики", "auction_mode_students"),
					tgbotapi.NewInlineKeyboardButtonData("🏫 Класс", "auction_mode_class"),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "auction_cancel"),
				),
			)
			edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, text, markup)
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		case AuctionStepClassLetter: // назад к номеру
			state.Step = AuctionStepClassNumber
			promptClassNumber(cq, bot, "auction_class_number_")
			return
		case AuctionStepStudentSelect: // назад к букве
			state.Step = AuctionStepClassLetter
			promptClassLetter(cq, bot, "auction_class_letter_")
			return
		case AuctionStepPoints: // назад к предыдущему выбору
			if state.Mode == "students" {
				state.Step = AuctionStepStudentSelect
				promptStudentSelect(cq, bot, database)
			} else {
				state.Step = AuctionStepClassLetter
				promptClassLetter(cq, bot, "auction_class_letter_")
			}
			return
		default:
			// safety: отмена
			delete(auctionStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Аукцион отменён.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
	}

	switch {
	case strings.HasPrefix(data, "auction_mode_"):
		state.Mode = strings.TrimPrefix(data, "auction_mode_")
		state.Step = AuctionStepClassNumber
		promptClassNumber(cq, bot, "auction_class_number_")

	case strings.HasPrefix(data, "auction_class_number_"):
		numStr := strings.TrimPrefix(data, "auction_class_number_")
		classNumber, _ := strconv.ParseInt(numStr, 10, 64)
		state.ClassNumber = classNumber
		state.Step = AuctionStepClassLetter
		promptClassLetter(cq, bot, "auction_class_letter_")

	case strings.HasPrefix(data, "auction_class_letter_"):
		letter := strings.TrimPrefix(data, "auction_class_letter_")
		state.ClassLetter = letter
		if state.Mode == "students" {
			state.Step = AuctionStepStudentSelect
			promptStudentSelect(cq, bot, database)
		} else if state.Mode == "class" {
			students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
			if len(students) == 0 { // стоп, идти дальше не к кому
				edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ В этом классе нет учеников.")
				if _, err := tg.Send(bot, edit); err != nil {
					metrics.HandlerErrors.Inc()
				}
				delete(auctionStates, chatID)
				return
			}
			for _, s := range students {
				state.SelectedStudentIDs = append(state.SelectedStudentIDs, s.ID)
			}
			state.Step = AuctionStepPoints
			promptPointsInput(cq, bot)
		}

	case strings.HasPrefix(data, "auction_select_student_"):
		idStr := strings.TrimPrefix(data, "auction_select_student_")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		// toggle
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
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Выберите хотя бы одного ученика.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		state.Step = AuctionStepPoints
		promptPointsInput(cq, bot)
	}
}

// ——— text step ———

func HandleAuctionText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := auctionStates[chatID]
	if state == nil || state.Step != AuctionStepPoints {
		return
	}

	// текстовая отмена
	if fsmutil.IsCancelText(msg.Text) {
		delete(auctionStates, chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Аукцион отменён.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	points, err := strconv.Atoi(strings.TrimSpace(msg.Text))
	if err != nil || points <= 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Введите корректное положительное число.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	key := fmt.Sprintf("auction:%d", chatID)
	if !fsmutil.SetPending(chatID, key) {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer fsmutil.ClearPending(chatID, key)

	state.PointsToRemove = points
	var notEnough []string
	var inactive []string
	eligible := make([]int64, 0, len(state.SelectedStudentIDs))
	for _, studentID := range state.SelectedStudentIDs {
		u, _ := db.GetUserByID(database, studentID)
		if u.ID == 0 || !u.IsActive {
			if u.ID != 0 && strings.TrimSpace(u.Name) != "" {
				inactive = append(inactive, u.Name)
			}
			continue
		}
		total, err := db.GetApprovedScoreSum(database, studentID)
		if err != nil {
			log.Println("❌ Ошибка при получении баллов:", err)
			continue
		}
		if total < points {
			notEnough = append(notEnough, u.Name)
		} else {
			eligible = append(eligible, studentID)
		}
	}

	if len(notEnough) > 0 {
		text := "❌ У следующих учеников недостаточно баллов:\n" + strings.Join(notEnough, "\n")
		if len(inactive) > 0 {
			text += "\n\n⚠️ Пропущены (неактивны): " + strings.Join(inactive, ", ")
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, text)); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(auctionStates, chatID)
		return
	}
	if len(eligible) == 0 {
		text := "❌ Некому списывать: нет ни одного активного ученика с достаточным количеством баллов."
		if len(inactive) > 0 {
			text += "\n⚠️ Пропущены (неактивны): " + strings.Join(inactive, ", ")
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, text)); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(auctionStates, chatID)
		return
	}
	user, err := db.GetUserByTelegramID(database, chatID)
	if err != nil {
		log.Println("❌ Ошибка получения пользователя:", err)
		return
	}
	_ = db.SetActivePeriod(database)
	period, err := db.GetActivePeriod(database)
	if err != nil || period == nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось определить активный период.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	comment := "Аукцион"
	catID := db.GetCategoryIDByName(database, "Аукцион")
	for _, studentID := range eligible {
		u, _ := db.GetUserByID(database, studentID)
		if u.ID == 0 || !u.IsActive {
			continue // пропускаем неактивных
		}
		score := models.Score{
			StudentID:  studentID,
			CategoryID: int64(catID), // спец-категория для аукциона
			Points:     points,
			Type:       "remove",
			Comment:    &comment,
			Status:     "pending",
			CreatedBy:  user.ID,
			CreatedAt:  time.Now(),
			PeriodID:   &period.ID,
		}
		_ = db.AddScore(database, score)

		NotifyAdminsAboutScoreRequest(bot, database, score)
	}

	msgOut := "✅ Заявка на аукцион создана и ожидает подтверждения."
	if len(inactive) > 0 {
		msgOut += "\n⚠️ Пропущены (неактивны): " + strings.Join(inactive, ", ")
	}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, msgOut)); err != nil {
		metrics.HandlerErrors.Inc()
	}
	delete(auctionStates, chatID)
}

// ——— menus (edit current message) ———

func promptClassNumber(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, prefix string) {
	chatID := cq.Message.Chat.ID
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		btn := tgbotapi.NewInlineKeyboardButtonData(strconv.Itoa(i), fmt.Sprintf("%s%d", prefix, i))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	rows = append(rows, auctionBackCancelRow())
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", tgbotapi.NewInlineKeyboardMarkup(rows...))
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func promptClassLetter(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, prefix string) {
	chatID := cq.Message.Chat.ID
	letters := []string{"А", "Б", "В", "Г", "Д"}
	row := []tgbotapi.InlineKeyboardButton{}
	for _, l := range letters {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(l, prefix+l))
	}
	rows := [][]tgbotapi.InlineKeyboardButton{row, auctionBackCancelRow()}
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "🔠 Выберите букву класса:", tgbotapi.NewInlineKeyboardMarkup(rows...))
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
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
	rows = append(rows, auctionBackCancelRow())

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, cq.Message.MessageID, "👥 Выберите учеников для аукциона:", tgbotapi.NewInlineKeyboardMarkup(rows...))
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func promptPointsInput(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI) {
	chatID := cq.Message.Chat.ID
	rows := [][]tgbotapi.InlineKeyboardButton{auctionBackCancelRow()}
	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID,
		cq.Message.MessageID,
		"✏️ Введите количество баллов для списания:",
		tgbotapi.NewInlineKeyboardMarkup(rows...),
	)
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// ——— accessors ———

func GetAuctionState(userID int64) *AuctionFSMState {
	return auctionStates[userID]
}
