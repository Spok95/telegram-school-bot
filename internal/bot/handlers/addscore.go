package handlers

// import (
//
//	"database/sql"
//	"fmt"
//	"github.com/Spok95/telegram-school-bot/internal/db"
//	"github.com/Spok95/telegram-school-bot/internal/models"
//	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
//	"log"
//	"strconv"
//	"strings"
//	"time"
//
// )
//
// const (
//
//	StepStudent  = "student"
//	StepCategory = "category"
//	StepValue    = "value"
//	StepComment  = "comment"
//	StepConfirm  = "confirm"
//
// )
//
//	type AddScoreFSM struct {
//		Step       string
//		StudentID  int64
//		CategoryID int64
//		Value      int
//		Comment    string
//	}
//
// var addScoreStates = make(map[int64]*AddScoreFSM)
//func GetAddScoreState(chatID int64) *AddScoreFSM {
//	state, ok := addScoreStates[chatID]
//	if !ok {
//		return nil
//	}
//	return state
//}

//
//// HandleAddScore запускает процесс добавления
//func HandleAddScore(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
//	user, err := db.GetUserByTelegramID(database, msg.From.ID)
//	if err != nil || user.Role == nil || (*user.Role != models.Teacher && *user.Role != models.Admin) {
//		sendText(bot, msg.Chat.ID, "❌ У вас нет прав для начисления баллов.")
//		return
//	}
//
//	// Получаем список учеников
//	students, err := db.GetAllStudents(database)
//	if err != nil || len(students) == 0 {
//		sendText(bot, msg.Chat.ID, "❌ Нет доступных учеников.")
//		return
//	}
//
//	// Сохраняем начальное состояние
//	addScoreStates[msg.Chat.ID] = &AddScoreFSM{Step: StepStudent}
//
//	// Формируем список учеников в кнопках
//	var rows [][]tgbotapi.InlineKeyboardButton
//	for _, student := range students {
//		label := fmt.Sprintf("%s", student.Name)
//		if label == "" {
//			label = fmt.Sprintf("ID %d", student.TelegramID)
//		}
//		button := tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("addscore_student_%d", student.TelegramID))
//		rows = append(rows, tgbotapi.NewInlineKeyboardRow(button))
//	}
//	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
//
//	msgText := tgbotapi.NewMessage(msg.Chat.ID, "👤 Выберите ученика:")
//	msgText.ReplyMarkup = keyboard
//	bot.Send(msgText)
//}
//
//func HandleAddScoreCallback(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
//	state, ok := addScoreStates[callback.Message.Chat.ID]
//	if !ok || state.Step != StepStudent {
//		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
//		return
//	}
//	if strings.HasPrefix(callback.Data, "addscore_student_") {
//		idStr := strings.TrimPrefix(callback.Data, "addscore_student_")
//		studentID, err := strconv.ParseInt(idStr, 10, 64)
//		if err != nil {
//			bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Ошибка ID ученика"))
//			return
//		}
//
//		state.StudentID = studentID
//		state.Step = StepCategory
//
//		// Переход к выбору категории
//		catList, err := db.GetAllCategories(database)
//		if err != nil {
//			bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "❌ Не удалось получить категории из базы."))
//			return
//		}
//		var rows [][]tgbotapi.InlineKeyboardButton
//		for _, c := range catList {
//			data := fmt.Sprintf("addscore_category_%d", c.ID)
//			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
//				tgbotapi.NewInlineKeyboardButtonData(c.Name, data)))
//		}
//
//		msg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "📚 Выберите категорию:")
//		msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
//		bot.Send(msg)
//	}
//}
//
//func HandleAddScoreCategory(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
//	chatID := callback.Message.Chat.ID
//	data := callback.Data
//
//	// Получаем текущее состояние
//	state, ok := addScoreStates[chatID]
//	if !ok || state.Step != StepCategory {
//		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
//		return
//	}
//
//	idStr := strings.TrimPrefix(data, "addscore_category_")
//	catID, err := strconv.Atoi(idStr)
//	if err != nil {
//		bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Неверный ID категории."))
//		return
//	}
//
//	state.CategoryID = int64(catID)
//	state.Step = StepValue
//
//	levels, err := db.GetLevelsByCategoryID(database, catID)
//	if err != nil || len(levels) == 0 {
//		bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет уровней для этой категории."))
//		return
//	}
//
//	var rows [][]tgbotapi.InlineKeyboardButton
//	for _, level := range levels {
//		btn := tgbotapi.NewInlineKeyboardButtonData(
//			fmt.Sprintf("%s (%d)", level.Label, level.Value),
//			fmt.Sprintf("addscore_level_%d", level.ID),
//		)
//		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
//	}
//
//	msg := tgbotapi.NewMessage(chatID, "🔢 Выберите уровень баллов:")
//	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
//	bot.Send(msg)
//	bot.Request(tgbotapi.NewCallback(callback.ID, "Категория выбрана"))
//}
//
//func HandleAddScoreLevel(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
//	chatID := callback.Message.Chat.ID
//	state, ok := addScoreStates[chatID]
//	if !ok || state.Step != StepValue {
//		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
//		return
//	}
//
//	idStr := strings.TrimPrefix(callback.Data, "addscore_level_")
//	levelID, err := strconv.Atoi(idStr)
//	if err != nil {
//		bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Неверный ID уровня."))
//		return
//	}
//
//	level, err := db.GetLevelByID(database, levelID)
//	if err != nil {
//		bot.Send(tgbotapi.NewMessage(chatID, "❌ Уровень не найден."))
//		return
//	}
//
//	state.Value = level.Value
//	state.Step = StepComment
//
//	bot.Send(tgbotapi.NewMessage(chatID, "✍️ Введите причину начисления:"))
//	bot.Request(tgbotapi.NewCallback(callback.ID, "Уровень выбран"))
//}
//
//func HandleAddScoreValue(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
//	chatID := msg.Chat.ID
//	text := msg.Text
//
//	state, ok := addScoreStates[chatID]
//	if !ok || state.Step != StepValue {
//		return
//	}
//
//	points, err := strconv.Atoi(text)
//	if err != nil {
//		bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите число."))
//		return
//	}
//
//	state.Value = points
//	state.Step = StepComment
//
//	bot.Send(tgbotapi.NewMessage(chatID, "✍️ Введите причину начисления:"))
//}
//
//func HandleAddScoreComment(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
//	chatID := msg.Chat.ID
//	comment := strings.TrimSpace(msg.Text)
//
//	state, ok := addScoreStates[chatID]
//	if !ok || state.Step != StepComment {
//		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Некорректный шаг. Попробуйте заново."))
//		return
//	}
//	state.Comment = comment
//	// Переход к следующему шагу
//	state.Step = StepConfirm
//	addScoreStates[chatID] = state
//
//	text := fmt.Sprintf("✅ Подтвердите начисление:\n\n"+
//		"👤 Ученик: %d\n"+
//		"📚 Категория: %d\n"+
//		"💯 Баллы: %d\n"+
//		"📝 Причина: %s",
//		state.StudentID,
//		state.CategoryID,
//		state.Value,
//		state.Comment,
//	)
//
//	confirmButtons := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "addscore_confirm"),
//			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "addscore_cancel"),
//		),
//	)
//
//	msgToSend := tgbotapi.NewMessage(chatID, text)
//	msgToSend.ParseMode = "Markdown"
//	msgToSend.ReplyMarkup = confirmButtons
//	if _, err := bot.Send(msgToSend); err != nil {
//		log.Println("Ошибка при отправке подтверждения:", err)
//	}
//}
//
//func HandleAddScoreConfirmCallback(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
//	chatID := callback.Message.Chat.ID
//	state, ok := addScoreStates[chatID]
//	if !ok || state.Step != StepConfirm {
//		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
//		return
//	}
//
//	score := models.Score{
//		StudentID:  state.StudentID,
//		CategoryID: state.CategoryID,
//		Points:     state.Value,
//		Type:       "add",
//		Comment:    &state.Comment,
//		Status:     "approved",
//		CreatedBy:  chatID,
//		CreatedAt:  time.Now(),
//	}
//
//	// Создание записи в базе
//	err := db.AddScore(database, score)
//	if err != nil {
//		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при сохранении баллов."))
//		log.Println("Ошибка записи в базу:", err)
//		return
//	}
//
//	bot.Send(tgbotapi.NewMessage(chatID, "✅ Баллы успешно начислены!"))
//	delete(addScoreStates, chatID)
//}
//
//func HandleAddScoreCancelCallback(bot *tgbotapi.BotAPI, callback *tgbotapi.CallbackQuery) {
//	chatID := callback.Message.Chat.ID
//	delete(addScoreStates, chatID)
//	bot.Send(tgbotapi.NewMessage(chatID, "❌ Начисление отменено."))
//}
//
//func sendText(bot *tgbotapi.BotAPI, chatID int64, text string) {
//	msg := tgbotapi.NewMessage(chatID, text)
//	bot.Send(msg)
//}
