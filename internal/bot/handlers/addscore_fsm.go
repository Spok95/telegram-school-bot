package handlers

import (
	"context"
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

type AddFSMState struct {
	Step               int
	ClassNumber        int64
	ClassLetter        string
	SelectedStudentIDs []int64
	CategoryID         int
	LevelID            int
	Comment            string
	RequestID          string
}

var addStates = make(map[int64]*AddFSMState)

// ==== helpers ====

func addBackCancelRow() []tgbotapi.InlineKeyboardButton {
	row := fsmutil.BackCancelRow("add_back", "add_cancel")
	return row
}

func backCancelRowFor(actionOrPrefix string) []tgbotapi.InlineKeyboardButton {
	// если передали "remove_class_letter_", вытащим "remove"
	action := actionOrPrefix
	if i := strings.Index(actionOrPrefix, "_"); i > 0 {
		action = actionOrPrefix[:i]
	}
	return fsmutil.BackCancelRow(action+"_back", action+"_cancel")
}

func addEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	if _, err := tg.Send(bot, cfg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func ClassNumberRows(action string) [][]tgbotapi.InlineKeyboardButton {
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, num := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
		callback := fmt.Sprintf("%s_class_num_%d", action, num)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", num), callback),
		))
	}
	buttons = append(buttons, backCancelRowFor(action))
	return buttons
}

func ClassLetterRows(action string) [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l, action+l),
		))
	}
	rows = append(rows, backCancelRowFor(action))
	return rows
}

// ==== start ====

func StartAddScoreFSM(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	// запрет неактивным
	u, _ := db.GetUserByTelegramID(ctx, database, chatID)
	if u == nil || !fsmutil.MustBeActiveForOps(u) {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Доступ временно закрыт. Обратитесь к администратору.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	delete(addStates, chatID)
	addStates[chatID] = &AddFSMState{
		Step:               1,
		SelectedStudentIDs: []int64{},
	}

	out := tgbotapi.NewMessage(chatID, "Выберите номер класса:")
	out.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(ClassNumberRows("add")...)
	if _, err := tg.Send(bot, out); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// ==== callbacks ====

func HandleAddScoreCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
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
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	// Обработка подтверждения (мгновенная запись)
	if strings.HasPrefix(data, "add_confirm:") {
		rid := strings.TrimPrefix(data, "add_confirm:")

		// простая проверка идемпотентности по request_id
		if rid == "" || rid != state.RequestID {
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			return
		}

		// one-shot защита на чат: если уже обрабатывается — игнор
		key := fmt.Sprintf("add_confirm:%s", rid)
		if !fsmutil.SetPending(chatID, key) {
			return
		}
		defer fsmutil.ClearPending(chatID, key)

		// погасим клавиатуру до операций, чтобы второй клик не сработал
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)

		level, _ := db.GetLevelByID(ctx, database, state.LevelID)
		user, _ := db.GetUserByTelegramID(ctx, database, chatID)
		var createdBy int64
		if user != nil {
			createdBy = user.ID
		} else {
			// Если по какой-то причине пользователя не нашли — фиксируем и выходим мягко
			log.Printf("HandleAddScoreCallback: user is nil for telegram id=%d", chatID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "⚠️ Не удалось определить пользователя. Попробуйте ещё раз.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			delete(addStates, chatID)
			return
		}
		now := time.Now()

		// Уточним активный период (не критично, AddScoreInstant сам подхватит, если есть)
		_ = db.SetActivePeriod(ctx, database)

		// Пропускаем неактивных на момент подтверждения
		var skipped []string
		for _, sid := range state.SelectedStudentIDs {
			u, _ := db.GetUserByID(ctx, database, sid)
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
				Type:       "add",
				CreatedBy:  createdBy,
			}
			// комментарий для начислений — опционален; в UX подтверждения мы его не спрашиваем
			trim := strings.TrimSpace(state.Comment)
			if trim != "" {
				c := trim
				score.Comment = &c
			}
			if err := db.AddScoreInstant(ctx, database, score, createdBy, now); err != nil {
				log.Printf("AddScoreInstant error student=%d: %v", sid, err)
			}
		}

		msgText := "✅ Баллы начислены. 30% учтены в коллективном рейтинге класса."
		if len(skipped) > 0 {
			msgText += "\n⚠️ Пропущены (неактивны): " + strings.Join(skipped, ", ")
		}
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, msgText)
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		delete(addStates, chatID)
		return
	}

	// ⬅ Назад
	if data == "add_back" {
		switch state.Step {
		case 2: // выбирали букву → вернёмся к номеру
			state.Step = 1
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите номер класса:", ClassNumberRows("add"))
			return
		case 3: // выбирали учеников → вернёмся к букве
			state.Step = 2
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", ClassLetterRows("add_class_letter_"))
			return
		case 4: // выбирали категорию → назад к ученикам
			state.Step = 3
			// пересоберём список учеников
			students, _ := db.GetStudentsByClass(ctx, database, state.ClassNumber, state.ClassLetter)
			var buttons [][]tgbotapi.InlineKeyboardButton
			for _, s := range students {
				label := s.Name
				if containsInt64(state.SelectedStudentIDs, s.ID) {
					label = "✅ " + label
				}
				callback := fmt.Sprintf("add_score_student_%d", s.ID)
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
			user, _ := db.GetUserByTelegramID(ctx, database, chatID)
			cats, _ := db.GetCategories(ctx, database, false)
			categories := make([]models.Category, 0, len(cats))
			role := ""
			if user != nil && user.Role != nil {
				role = string(*user.Role)
			}
			for _, c := range cats {
				if role != "admin" && role != "administration" && c.Name == "Аукцион" {
					continue
				}
				categories = append(categories, c)
			}
			var buttons [][]tgbotapi.InlineKeyboardButton
			for _, c := range categories {
				callback := fmt.Sprintf("add_score_category_%d", c.ID)
				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(c.Name, callback),
				))
			}
			buttons = append(buttons, addBackCancelRow())
			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите категорию:", buttons)
			return
		case 6: // ввод комментария → назад к уровню
			state.Step = 5
			levels, _ := db.GetLevelsByCategoryIDFull(ctx, database, int64(state.CategoryID), false)
			var buttons [][]tgbotapi.InlineKeyboardButton
			for _, l := range levels {
				callback := fmt.Sprintf("add_score_level_%d", l.ID)
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
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
	}

	// ==== обычные ветки ====

	if strings.HasPrefix(data, "add_class_num_") {
		numStr := strings.TrimPrefix(data, "add_class_num_")
		num, _ := strconv.ParseInt(numStr, 10, 64)
		state.ClassNumber = num
		state.Step = 2

		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", ClassLetterRows("add_class_letter_"))
		return
	}

	if strings.HasPrefix(data, "add_class_letter_") {
		state.ClassLetter = strings.TrimPrefix(data, "add_class_letter_")
		state.Step = 3

		students, _ := db.GetStudentsByClass(ctx, database, state.ClassNumber, state.ClassLetter)
		if len(students) == 0 {
			delete(addStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ В этом классе нет учеников.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			callback := fmt.Sprintf("add_score_student_%d", s.ID)
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

	if strings.HasPrefix(data, "add_score_student_") || data == "add_select_all_students" {
		idStr := strings.TrimPrefix(data, "add_score_student_")
		id, _ := strconv.ParseInt(idStr, 10, 64)

		if data != "add_select_all_students" {
			// toggle: если уже выбран — снимаем
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
			students, _ := db.GetStudentsByClass(ctx, database, state.ClassNumber, state.ClassLetter)
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

		// пересобираем клавиатуру
		students, _ := db.GetStudentsByClass(ctx, database, state.ClassNumber, state.ClassLetter)
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, s := range students {
			label := s.Name
			if containsInt64(state.SelectedStudentIDs, s.ID) {
				label = "✅ " + label
			}
			callback := fmt.Sprintf("add_score_student_%d", s.ID)
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
		user, _ := db.GetUserByTelegramID(ctx, database, chatID)
		cats, _ := db.GetCategories(ctx, database, false) // только активные
		categories := make([]models.Category, 0, len(cats))
		role := ""
		if user != nil && user.Role != nil {
			role = string(*user.Role)
		}

		for _, c := range cats {
			if role != "admin" && role != "administration" && c.Name == "Аукцион" {
				continue
			}
			categories = append(categories, c)
		}

		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			callback := fmt.Sprintf("add_score_category_%d", c.ID)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c.Name, callback),
			))
		}
		buttons = append(buttons, addBackCancelRow())
		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите категорию:", buttons)
		return
	}

	if strings.HasPrefix(data, "add_score_category_") {
		catID, _ := strconv.Atoi(strings.TrimPrefix(data, "add_score_category_"))
		state.CategoryID = catID
		state.Step = 5
		levels, _ := db.GetLevelsByCategoryIDFull(ctx, database, int64(state.CategoryID), false)
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, l := range levels {
			callback := fmt.Sprintf("add_score_level_%d", l.ID)
			label := fmt.Sprintf("%s (%d)", l.Label, l.Value)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, callback),
			))
		}
		buttons = append(buttons, addBackCancelRow())
		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите уровень:", buttons)
		return
	}

	if strings.HasPrefix(data, "add_score_level_") {
		lvlID, _ := strconv.Atoi(strings.TrimPrefix(data, "add_score_level_"))
		state.LevelID = lvlID
		state.Step = 6

		// === Новый шаг: карточка подтверждения (без текстового комментария) ===

		// уровень
		level, _ := db.GetLevelByID(ctx, database, state.LevelID)
		points := level.Value

		// имя категории (без отдельного метода — через общий список)
		catName := fmt.Sprintf("Категория #%d", state.CategoryID)
		if cats, err := db.GetCategories(ctx, database, false); err == nil {
			for _, c := range cats {
				if c.ID == state.CategoryID {
					catName = c.Name
					break
				}
			}
		}

		period, err := db.GetActivePeriod(ctx, database)
		if err != nil || period == nil {
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ Нет активного периода. Установите активный период и попробуйте снова.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
			delete(addStates, chatID)
			return
		}

		// имена учеников
		var names []string
		for _, sid := range state.SelectedStudentIDs {
			u, err := db.GetUserByID(ctx, database, sid)
			if err != nil || u.ID == 0 || strings.TrimSpace(u.Name) == "" {
				names = append(names, fmt.Sprintf("ID:%d", sid))
			} else {
				names = append(names, u.Name)
			}
		}

		state.RequestID = fmt.Sprintf("%d_%d", chatID, time.Now().UnixNano())

		text := fmt.Sprintf(
			"Подтверждение начисления\n\nКласс: %d%s\nКатегория: %s\nКоличество баллов: %d\nУченики:\n• %s\n\nПодтвердить начисление?",
			state.ClassNumber, state.ClassLetter, catName, points, strings.Join(names, "\n• "),
		)
		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Да", "add_confirm:"+state.RequestID),
			),
			addBackCancelRow(),
		}
		addEditMenu(bot, chatID, cq.Message.MessageID, text, rows)
		return
	}
}

// ==== текстовый шаг ====

func HandleAddScoreText(ctx context.Context, bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	chatID := msg.Chat.ID
	state, ok := addStates[chatID]
	if !ok {
		return
	}

	if state.Step == 6 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Нажмите «✅ Да» или используйте «Назад/Отмена» ниже.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	delete(addStates, chatID)
}

// GetAddScoreState доступ из main.go
func GetAddScoreState(chatID int64) *AddFSMState {
	return addStates[chatID]
}
