package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
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
	Step                 int
	ClassNumber          int64
	ClassLetter          string
	SelectedStudentIDs   []int64
	CategoryID           int
	LevelID              int
	Comment              string
	RequestID            string
	CategoryName         string
	LevelLabel           string
	LevelValue           int
	SelectedStudentNames []string
	MessageID            int
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
	if _, err := tg.Send(bot, cfg); err != nil {
		metrics.HandlerErrors.Inc()
	}
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

	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		out.Text = "Нет доступных классов для начисления."
		if _, err := tg.Send(bot, out); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	// соберём уникальные номера
	numsSet := make(map[int]struct{})
	for _, c := range classes {
		numsSet[c.Number] = struct{}{}
	}
	var nums []int
	for n := range numsSet {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, n := range nums {
		cb := fmt.Sprintf("add_class_num_%d", n)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", n), cb),
		))
	}
	rows = append(rows, addBackCancelRow())

	out.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
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

	// Ввод комментария (опционально)
	if data == "add_comment" {
		state.Step = 7
		rows := [][]tgbotapi.InlineKeyboardButton{addBackCancelRow()}
		addEditMenu(bot, chatID, cq.Message.MessageID, "Введите комментарий (необязательно):", rows)
		return
	}

	// ⬅ Назад
	if data == "add_back" {
		switch state.Step {
		case 2:
			state.Step = 1

			classes, err := db.ListVisibleClasses(ctx, database)
			if err != nil || len(classes) == 0 {
				addEditMenu(bot, chatID, cq.Message.MessageID, "Нет доступных классов для начисления.", [][]tgbotapi.InlineKeyboardButton{addBackCancelRow()})
				return
			}

			numsSet := make(map[int]struct{})
			for _, c := range classes {
				numsSet[c.Number] = struct{}{}
			}
			var nums []int
			for n := range numsSet {
				nums = append(nums, n)
			}
			sort.Ints(nums)

			var rows [][]tgbotapi.InlineKeyboardButton
			for _, n := range nums {
				cb := fmt.Sprintf("add_class_num_%d", n)
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", n), cb),
				))
			}
			rows = append(rows, addBackCancelRow())

			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите номер класса:", rows)
			return
		case 3: // выбирали учеников → вернёмся к букве
			state.Step = 2

			classes, err := db.ListVisibleClasses(ctx, database)
			if err != nil || len(classes) == 0 {
				addEditMenu(bot, chatID, cq.Message.MessageID, "Нет букв для этого класса.", [][]tgbotapi.InlineKeyboardButton{addBackCancelRow()})
				return
			}

			var rows [][]tgbotapi.InlineKeyboardButton
			for _, c := range classes {
				if int64(c.Number) != state.ClassNumber {
					continue
				}
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(strings.ToUpper(c.Letter), "add_class_letter_"+strings.ToUpper(c.Letter)),
				))
			}
			rows = append(rows, addBackCancelRow())

			addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", rows)
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
		case 6: // карточка подтверждения → назад к выбору уровня
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
		case 7: // ввод комментария → назад к карточке подтверждения
			state.Step = 6
			renderAddConfirm(bot, chatID, cq.Message.MessageID, state)
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

		// тянем видимые классы и рисуем только буквы этого номера
		classes, err := db.ListVisibleClasses(ctx, database)
		if err != nil || len(classes) == 0 {
			addEditMenu(bot, chatID, cq.Message.MessageID, "Нет букв для этого класса.", [][]tgbotapi.InlineKeyboardButton{addBackCancelRow()})
			return
		}

		var rows [][]tgbotapi.InlineKeyboardButton
		for _, c := range classes {
			if int64(c.Number) != state.ClassNumber {
				continue
			}
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(strings.ToUpper(c.Letter), "add_class_letter_"+strings.ToUpper(c.Letter)),
			))
		}
		rows = append(rows, addBackCancelRow())

		addEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", rows)
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

		// === Карточка подтверждения (теперь с опциональным комментарием) ===

		// уровень
		level, _ := db.GetLevelByID(ctx, database, state.LevelID)
		state.LevelLabel = level.Label
		state.LevelValue = level.Value

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
		state.CategoryName = catName

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
		state.SelectedStudentNames = names

		state.RequestID = fmt.Sprintf("%d_%d", chatID, time.Now().UnixNano())
		state.MessageID = cq.Message.MessageID

		// рендер карточки подтверждения
		renderAddConfirm(bot, chatID, cq.Message.MessageID, state)
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

	if state.Step == 7 {
		// ввод опционального комментария
		if fsmutil.IsCancelText(msg.Text) {
			delete(addStates, chatID)
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Начисление отменено.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		trimmed := strings.TrimSpace(msg.Text)
		state.Comment = trimmed // пусто — значит без комментария
		state.Step = 6
		// перерисуем карточку подтверждения, показывая (если есть) комментарий
		// если MessageID потерян, безопасно проигнорируем (но мы его ставим при карточке)
		mid := state.MessageID
		if mid == 0 {
			mid = msg.MessageID
		}
		renderAddConfirm(bot, chatID, mid, state)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Нажмите «✅ Да» или используйте «Назад/Отмена» ниже.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
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

// renderAddConfirm — единый рендер карточки подтверждения начисления.
// Использует только состояние (без доступа к БД).
func renderAddConfirm(bot *tgbotapi.BotAPI, chatID int64, messageID int, state *AddFSMState) {
	text := fmt.Sprintf(
		"Подтвердите начисление баллов:\n\nКласс: %d%s\nУченики: %s\nКатегория: %s\nУровень: %s (%d)\nБаллы: %d",
		state.ClassNumber, state.ClassLetter, strings.Join(state.SelectedStudentNames, ", "),
		state.CategoryName, state.LevelLabel, state.LevelValue, state.LevelValue,
	)
	if trim := strings.TrimSpace(state.Comment); trim != "" {
		text += "\nКомментарий: " + trim
	}
	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Комментарий", "add_comment"),
			tgbotapi.NewInlineKeyboardButtonData("✅ Да", "add_confirm:"+state.RequestID),
		),
	}
	rows = append(rows, addBackCancelRow())
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = &markup
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}
