package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"strconv"
	"strings"
)

type AddScoreFSM struct {
	Step      string
	StudentID int64
	Category  string
	Value     int
	Comment   string
}

var addScoreStates = make(map[int64]*AddScoreFSM)

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
	addScoreStates[msg.Chat.ID] = &AddScoreFSM{Step: "student"}

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

func HandleAddScoreCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	state, ok := addScoreStates[cb.Message.Chat.ID]
	if !ok || state.Step != "student" {
		bot.Request(tgbotapi.NewCallback(cb.ID, "⚠️ Некорректный шаг."))
		return
	}
	if strings.HasPrefix(cb.Data, "addscore_student_") {
		idStr := strings.TrimPrefix(cb.Data, "addscore_student_")
		studentID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Ошибка ID ученика"))
			return
		}

		state.StudentID = studentID
		state.Step = "category"

		// Переход к выбору категории
		categories := []string{"Работа на уроке", "Курсы по выбору", "Внеурочная активность", "Социальные поступки", "Дежурство"}
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, c := range categories {
			data := fmt.Sprintf("addscore_category_%s", c)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(c, data)))
		}

		msg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "📚 Выберите категорию:")
		msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
		bot.Send(msg)
	}
}
