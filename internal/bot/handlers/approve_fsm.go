package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ShowPendingScores показывает администратору все заявки с status = 'pending'
func ShowPendingScores(bot *tgbotapi.BotAPI, database *sql.DB, adminID int64) {
	scores, err := db.GetPendingScores(database)
	if err != nil {
		log.Println("ошибка при получении заявок на баллы:", err)
		bot.Send(tgbotapi.NewMessage(adminID, "Ошибка при получении заявок на баллы."))
		return
	}
	if len(scores) == 0 {
		bot.Send(tgbotapi.NewMessage(adminID, "✅ Нет ожидающих подтверждения заявок."))
		return
	}

	for _, s := range scores {
		student, err1 := db.GetUserByID(database, s.StudentID)
		creator, err2 := db.GetUserByID(database, s.CreatedBy)

		if err1 != nil || err2 != nil {
			log.Println("Ошибка получения данных пользователя:", err1, err2)
			continue
		}
		comment := "(нет)"
		if s.Comment != nil && *s.Comment != "" {
			comment = *s.Comment
		}
		class := fmt.Sprintf("%d%s", *student.ClassNumber, *student.ClassLetter)
		text := fmt.Sprintf("Заявка от %s\n👤 Ученик: %s\n🏫 Класс: %s\n📚 Категория: %s\n💯 Баллы: %d (%s)\n📝 Комментарий: %s",
			creator.Name, student.Name, class, s.CategoryLabel, s.Points, s.Type, comment)

		approveBtn := tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("score_confirm_%d", s.ID))
		rejectBtn := tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", fmt.Sprintf("score_reject_%d", s.ID))
		markup := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(approveBtn, rejectBtn))

		msg := tgbotapi.NewMessage(adminID, text)
		msg.ReplyMarkup = markup
		bot.Send(msg)
	}
	delete(notifiedAdmins, adminID)
}

// HandleScoreApprovalCallback обрабатывает нажатия на кнопки подтверждения/отклонения заявок
func HandleScoreApprovalCallback(callback *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB, userID int64) {
	data := callback.Data
	var action, idStr string

	switch {
	case strings.HasPrefix(data, "score_confirm_"):
		action = "approve"
		idStr = strings.TrimPrefix(data, "score_confirm_")
	case strings.HasPrefix(data, "score_reject_"):
		action = "reject"
		idStr = strings.TrimPrefix(data, "score_reject_")
	default:
		return
	}
	scoreID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		log.Println("неверный ID заявки:", err)
		return
	}

	chatID := callback.Message.Chat.ID
	messageID := callback.Message.MessageID
	user, _ := db.GetUserByTelegramID(database, userID)

	var resultText string
	// Проверяем текущий статус
	currentStatus, err := db.GetScoreStatusByID(database, scoreID)
	if err != nil {
		log.Println("ошибка получения статуса заявки:", err)
		resultText = "❌ Ошибка при обработке заявки."
	} else if currentStatus != "pending" {
		resultText = "⏳ Заявка уже обработана ранее."
	} else if action == "approve" {
		err = db.ApproveScore(database, scoreID, userID, time.Now())
		if err == nil {
			resultText = fmt.Sprintf("✅ Заявка подтверждена.\nПодтвердил: @%s", user.Name)
		} else {
			log.Println("ошибка подтверждения заявки:", err)
			resultText = "❌ Ошибка при подтверждении заявки."
		}
	} else {
		err = db.RejectScore(database, scoreID, userID, time.Now())
		if err == nil {
			resultText = fmt.Sprintf("❌ Заявка отклонена.\nОтклонил: @%s", user.Name)
		} else {
			log.Println("ошибка отклонения заявки:", err)
			resultText = "❌ Ошибка при отклонении заявки."
		}
	}

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, resultText, tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{},
	})
	bot.Send(edit)

	bot.Request(tgbotapi.NewCallback(callback.ID, "Обработано"))
}
