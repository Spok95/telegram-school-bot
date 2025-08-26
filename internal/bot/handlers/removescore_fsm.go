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

// ===== helpers

func removeBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("remove_back", "remove_cancel")
}

func removeEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	bot.Send(cfg)
}

func containsInt64(slice []int64, v int64) bool {
	for _, x := range slice {
		if x == v {
			return true
		}
	}
	return false
}

// ===== start

func StartRemoveScoreFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	// запрет неактивным
	u, _ := db.GetUserByTelegramID(database, chatID)
	if u == nil || !fsmutil.MustBeActiveForOps(u) {
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Доступ временно закрыт. Обратитесь к администратору."))
		return
	}
	delete(removeStates, chatID)
	removeStates[chatID] = &RemoveFSMState{
		Step:               1,
		SelectedStudentIDs: []int64{},
	}

	out := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
	out.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(ClassNumberRows()...)
	bot.Send(out)
}

// ===== callbacks

func HandleRemoveCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.From.ID
	state, ok := removeStates[chatID]
	if !ok {
		return
	}
	data := cq.Data

	// ❌ Отмена — погасить клавиатуру у ЭТОГО сообщения и заменить текст
	if data == "remove_cancel" {
		delete(removeStates, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Списание отменено.")
		bot.Send(edit)
		return
	}

	// ⬅ Назад
	if data == "remove_back" {
		switch state.Step {
		case 2: // возвращаемся к выбору номера
			state.Step = 1
			removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите номер класса:", ClassNumberRows())
			return
		case 3: // назад к букве
			state.Step = 2
			removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", ClassLetterRows("remove_class_letter_"))
			return
		case 4: // назад к ученикам
			state.Step = 3
			students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
			var rows [][]tgbotapi.InlineKeyboardButton
			for _, s := range students {
				label := s.Name
				if containsInt64(state.SelectedStudentIDs, s.ID) {
					label = "✅ " + label
				}
				cb := fmt.Sprintf("remove_student_%d", s.ID)
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(label, cb),
				))
			}
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Выбрать всех", "remove_select_all_students"),
			))
			rows = append(rows, removeBackCancelRow())
			removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите ученика или учеников:", rows)
			return
		case 5: // назад к категориям
			state.Step = 4
			user, _ := db.GetUserByTelegramID(database, chatID)
			cats, _ := db.GetCategories(database, false)
			categories := make([]models.Category, 0, len(cats))
			role := string(*user.Role)
			for _, c := range cats {
				if role != "admin" && role != "administration" && c.Name == "Аукцион" {
					continue
				}
				categories = append(categories, c)
			}
			var rows [][]tgbotapi.InlineKeyboardButton
			for _, c := range categories {
				cb := fmt.Sprintf("remove_category_%d", c.ID)
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(c.Name, cb),
				))
			}
			rows = append(rows, removeBackCancelRow())
			removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите категорию:", rows)
			return
		case 6: // текстовый комментарий → назад к уровням
			state.Step = 5
			levels, _ := db.GetLevelsByCategoryIDFull(database, int64(state.CategoryID), false)
			var rows [][]tgbotapi.InlineKeyboardButton
			for _, l := range levels {
				cb := fmt.Sprintf("remove_level_%d", l.ID)
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s (%d)", l.Label, l.Value), cb),
				))
			}
			rows = append(rows, removeBackCancelRow())
			removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите уровень:", rows)
			return
		default:
			delete(removeStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Списание отменено.")
			bot.Send(edit)
			return
		}
	}

	// ===== обычные ветки

	if strings.HasPrefix(data, "remove_class_num_") {
		numStr := strings.TrimPrefix(data, "remove_class_num_")
		num, _ := strconv.ParseInt(numStr, 10, 64)
		state.ClassNumber = num
		state.Step = 2
		removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", ClassLetterRows("remove_class_letter_"))
		return
	}

	if strings.HasPrefix(data, "remove_class_letter_") {
		state.ClassLetter = strings.TrimPrefix(data, "remove_class_letter_")
		state.Step = 3

		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
		if len(students) == 0 {
			delete(removeStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ В этом классе нет учеников.")
			bot.Send(edit)
			return
		}

		var rows [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			cb := fmt.Sprintf("remove_student_%d", s.ID)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(s.Name, cb),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Выбрать всех", "remove_select_all_students"),
		))
		rows = append(rows, removeBackCancelRow())
		removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите ученика или учеников:", rows)
		return
	}

	if strings.HasPrefix(data, "remove_student_") || data == "remove_select_all_students" {
		idStr := strings.TrimPrefix(data, "remove_student_")
		id, _ := strconv.ParseInt(idStr, 10, 64)

		if data != "remove_select_all_students" {
			// toggle
			removed := false
			for i, sid := range state.SelectedStudentIDs {
				if sid == id {
					state.SelectedStudentIDs = append(state.SelectedStudentIDs[:i], state.SelectedStudentIDs[i+1:]...)
					removed = true
					break
				}
			}
			if !removed {
				state.SelectedStudentIDs = append(state.SelectedStudentIDs, id)
			}
		} else {
			// выбрать всех
			students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
			for _, s := range students {
				found := false
				for _, sid := range state.SelectedStudentIDs {
					if sid == s.ID {
						found = true
						break
					}
				}
				if !found {
					state.SelectedStudentIDs = append(state.SelectedStudentIDs, s.ID)
				}
			}
		}

		// пересоберём список
		students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			label := s.Name
			if containsInt64(state.SelectedStudentIDs, s.ID) {
				label = "✅ " + label
			}
			cb := fmt.Sprintf("remove_student_%d", s.ID)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, cb),
			))
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Выбрать всех", "remove_select_all_students"),
		))
		if len(state.SelectedStudentIDs) > 0 {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "remove_students_done"),
			))
		}
		rows = append(rows, removeBackCancelRow())
		removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите ученика или учеников:", rows)
		return
	}

	if data == "remove_students_done" {
		state.Step = 4
		user, _ := db.GetUserByTelegramID(database, chatID)
		cats, _ := db.GetCategories(database, false) // только активные
		categories := make([]models.Category, 0, len(cats))
		role := string(*user.Role)
		for _, c := range cats {
			if role != "admin" && role != "administration" && c.Name == "Аукцион" {
				continue
			}
			categories = append(categories, c)
		}
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			cb := fmt.Sprintf("remove_category_%d", c.ID)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c.Name, cb),
			))
		}
		rows = append(rows, removeBackCancelRow())
		removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите категорию:", rows)
		return
	}

	if strings.HasPrefix(data, "remove_category_") {
		catID, _ := strconv.Atoi(strings.TrimPrefix(data, "remove_category_"))
		state.CategoryID = catID
		state.Step = 5

		levels, _ := db.GetLevelsByCategoryIDFull(database, int64(state.CategoryID), false)
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, l := range levels {
			cb := fmt.Sprintf("remove_level_%d", l.ID)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s (%d)", l.Label, l.Value), cb),
			))
		}
		rows = append(rows, removeBackCancelRow())
		removeEditMenu(bot, chatID, cq.Message.MessageID, "Выберите уровень:", rows)
		return
	}

	if strings.HasPrefix(data, "remove_level_") {
		lvlID, _ := strconv.Atoi(strings.TrimPrefix(data, "remove_level_"))
		state.LevelID = lvlID
		state.Step = 6

		// комментарий обязателен — сразу подсказываем
		rows := [][]tgbotapi.InlineKeyboardButton{removeBackCancelRow()}
		removeEditMenu(bot, chatID, cq.Message.MessageID, "Введите комментарий (обязателен для списания):", rows)
		return
	}
}

// ===== text step

func HandleRemoveText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state, ok := removeStates[chatID]
	if !ok || state.Step != 6 {
		return
	}

	// поддержка текстовой отмены
	if fsmutil.IsCancelText(msg.Text) {
		delete(removeStates, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Списание отменено."))
		return
	}

	trimmed := strings.TrimSpace(msg.Text)
	if trimmed == "" {
		// комментарий обязателен
		rows := [][]tgbotapi.InlineKeyboardButton{removeBackCancelRow()}
		p := tgbotapi.NewMessage(chatID, "⚠️ Комментарий обязателен. Введите причину списания:")
		p.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		bot.Send(p)
		return
	}
	state.Comment = trimmed

	// one‑shot
	key := fmt.Sprintf("remove:%d", chatID)
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
		delete(removeStates, chatID)
		return
	}

	var skipped []string
	for _, sid := range state.SelectedStudentIDs {
		u, _ := db.GetUserByID(database, sid)
		if u.ID == 0 || !u.IsActive {
			if u.ID != 0 && strings.TrimSpace(u.Name) != "" {
				skipped = append(skipped, u.Name)
			}
			continue
		}
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
		_ = db.AddScore(database, score)

		student, err := db.GetUserByID(database, sid)
		if err != nil {
			log.Println("Ошибка получения ученика:", err)
			continue
		}
		NotifyAdminsAboutScoreRequest(bot, database, score, student.Name)
	}

	msgText := "Заявки на списание баллов отправлены на подтверждение."
	if len(skipped) > 0 {
		msgText += "\n⚠️ Пропущены (неактивны): " + strings.Join(skipped, ", ")
	}
	bot.Send(tgbotapi.NewMessage(chatID, msgText))
	delete(removeStates, chatID)
}

// доступ из main.go
func GetRemoveScoreState(chatID int64) *RemoveFSMState {
	return removeStates[chatID]
}
