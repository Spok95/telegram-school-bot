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
	StepStudent  = "student"
	StepCategory = "category"
	StepValue    = "value"
	StepComment  = "comment"
	StepConfirm  = "confirm"
)

type AddScoreFSM struct {
	Step      string
	StudentID int64
	Category  string
	Value     int
	Comment   string
}

var addScoreStates = make(map[int64]*AddScoreFSM)

func GetAddScoreState(chatID int64) *AddScoreFSM {
	state, ok := addScoreStates[chatID]
	if !ok {
		return nil
	}
	return state
}

// HandleAddScore запускает процесс добавления
func HandleAddScore(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	user, err := db.GetUserByTelegramID(database, msg.From.ID)
	if err != nil || user.Role == nil || (*user.Role != models.Teacher && *user.Role != models.Admin) {
		sendText(bot, msg.Chat.ID, "❌ У вас нет прав для начисления баллов.")
		return
	}

	// Получаем список учеников
	students, err := db.GetAllStudents(database)
	if err != nil || len(students) == 0 {
		sendText(bot, msg.Chat.ID, "❌ Нет доступных учеников.")
		return
	}

	// Сохраняем начальное состояние
	addScoreStates[msg.Chat.ID] = &AddScoreFSM{Step: StepStudent}

	// Формируем список учеников в кнопках
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, student := range students {
		label := fmt.Sprintf("%s", student.Name)
		if label == "" {
			label = fmt.Sprintf("ID %d", student.TelegramID)
		}
		button := tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("addscore_student_%d", student.TelegramID))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(button))
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msgText := tgbotapi.NewMessage(msg.Chat.ID, "👤 Выберите ученика:")
	msgText.ReplyMarkup = keyboard
	bot.Send(msgText)
}

func HandleAddScoreCallback(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
	state, ok := addScoreStates[callback.Message.Chat.ID]
	if !ok || state.Step != StepStudent {
		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
		return
	}
	if strings.HasPrefix(callback.Data, "addscore_student_") {
		idStr := strings.TrimPrefix(callback.Data, "addscore_student_")
		studentID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Ошибка ID ученика"))
			return
		}

		state.StudentID = studentID
		state.Step = StepCategory

		// Переход к выбору категории
		categories := []string{"Работа на уроке", "Курсы по выбору", "Внеурочная активность", "Социальные поступки", "Дежурство"}
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			data := fmt.Sprintf("addscore_category_%s", c)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c, data)))
		}

		msg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "📚 Выберите категорию:")
		msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
		bot.Send(msg)
	}
}

func HandleAddScoreCategory(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
	chatID := callback.Message.Chat.ID
	data := callback.Data

	// Получаем текущее состояние
	state, ok := addScoreStates[chatID]
	if !ok || state.Step != StepCategory {
		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
		return
	}

	categoryMap := map[string]string{
		"Работа на уроке":       "work",
		"Курсы по выбору":       "elective",
		"Внеурочная активность": "activity",
		"Социальные поступки":   "social",
		"Дежурство":             "duty",
	}

	label := strings.TrimPrefix(data, "addscore_category_")
	key, ok := categoryMap[label]
	if !ok {
		bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Неизвестная категория."))
		return
	}
	state.Category = key
	state.Step = StepValue

	// Переходим к следующему шагу — запрос баллов
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✏️ Введите количество баллов для категории: *%s*", label))
	msg.ParseMode = "Markdown"

	if _, err := bot.Send(msg); err != nil {
		log.Println("Ошибка при отправке запроса баллов:", err)
	}

	// Закрыть предыдущий callback (чтобы не висел индикатор загрузки)
	bot.Request(tgbotapi.NewCallback(callback.ID, "Категория выбрана"))
}

func HandleAddScoreValue(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := msg.Text

	state, ok := addScoreStates[chatID]
	if !ok || state.Step != StepValue {
		return
	}

	points, err := strconv.Atoi(text)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите число."))
		return
	}

	state.Value = points
	state.Step = StepComment

	bot.Send(tgbotapi.NewMessage(chatID, "✍️ Введите причину начисления:"))
}

func HandleAddScoreComment(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	comment := strings.TrimSpace(msg.Text)

	state, ok := addScoreStates[chatID]
	if !ok || state.Step != StepComment {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Некорректный шаг. Попробуйте заново."))
		return
	}
	state.Comment = comment
	// Переход к следующему шагу
	state.Step = StepConfirm
	addScoreStates[chatID] = state

	text := fmt.Sprintf("✅ Подтвердите начисление:\n\n"+
		"👤 Ученик: %d\n"+
		"📚 Категория: %s\n"+
		"💯 Баллы: %d\n"+
		"📝 Причина: %s",
		state.StudentID,
		categoryToLabel(state.Category),
		state.Value,
		state.Comment,
	)

	confirmButtons := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "addscore_confirm"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "addscore_cancel"),
		),
	)

	msgToSend := tgbotapi.NewMessage(chatID, text)
	msgToSend.ParseMode = "Markdown"
	msgToSend.ReplyMarkup = confirmButtons
	if _, err := bot.Send(msgToSend); err != nil {
		log.Println("Ошибка при отправке подтверждения:", err)
	}
}

func HandleAddScoreConfirmCallback(bot *tgbotapi.BotAPI, database *sql.DB, callback *tgbotapi.CallbackQuery) {
	chatID := callback.Message.Chat.ID
	state, ok := addScoreStates[chatID]
	if !ok || state.Step != StepConfirm {
		bot.Request(tgbotapi.NewCallback(callback.ID, "⚠️ Некорректный шаг."))
		return
	}

	score := models.Score{
		StudentID: state.StudentID,
		Category:  state.Category,
		Points:    state.Value,
		Type:      "add",
		Comment:   &state.Comment,
		Approved:  true,
		CreatedBy: chatID,
		CreatedAt: time.Now(),
	}

	// Создание записи в базе
	err := db.AddScore(database, score)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при сохранении баллов."))
		log.Println("Ошибка записи в базу:", err)
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, "✅ Баллы успешно начислены!"))
	delete(addScoreStates, chatID)
}

func HandleAddScoreCancelCallback(bot *tgbotapi.BotAPI, callback *tgbotapi.CallbackQuery) {
	chatID := callback.Message.Chat.ID
	delete(addScoreStates, chatID)
	bot.Send(tgbotapi.NewMessage(chatID, "❌ Начисление отменено."))
}
