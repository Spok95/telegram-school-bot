package handlers

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleMyScore(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	user, err := db.GetUserByTelegramID(database, chatID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден."))
		return
	}

	var targetID = user.ID

	// Если родитель — ищем telegram_id ребёнка
	if *user.Role == models.Parent {
		var studentInternalID int64
		err := database.QueryRow(`
			SELECT student_id FROM parents_students WHERE parent_id = $1
		`, user.ID).Scan(&studentInternalID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось найти привязанного ученика."))
			return
		}

		targetID = studentInternalID
	}

	// Получаем категории и суммы
	rows, err := database.Query(`
		SELECT c.label, SUM(s.points) as total
		FROM scores s
		JOIN categories c ON s.category_id = c.id
		WHERE s.student_id = $1 AND s.status = 'approved'
		GROUP BY s.category_id
	`, targetID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при получении рейтинга."))
		return
	}
	defer rows.Close()

	summary := make(map[string]int)
	total := 0

	for rows.Next() {
		var label string
		var sum int
		if err := rows.Scan(&label, &sum); err != nil {
			continue
		}
		summary[label] = sum
		total += sum
	}

	// Формируем текст ответа
	text := fmt.Sprintf("📊 Ваш общий рейтинг: %d баллов\n\n", total)
	for label, val := range summary {
		text += fmt.Sprintf("▫️ %s: %d\n", label, val)
	}

	// Получаем все начисления/списания
	history, err := db.GetScoreByStudent(database, targetID)
	if err != nil {
		log.Println("ошибка при получении истории:", err)
	} else {
		if len(history) > 0 {
			text += "\n\n📖 История:\n"
			count := 0
			for _, s := range history {
				if s.Status != "approved" {
					continue
				}
				sign := "+"
				if s.Type == "remove" {
					sign = "-"
				}
				date := s.CreatedAt.Format("02.01.2006")
				reason := "-"
				if s.Comment != nil && *s.Comment != "" {
					reason = *s.Comment
				}

				if reason == "-" {
					text += fmt.Sprintf("%s%d %s (%s)\n", sign, abs(s.Points), s.CategoryLabel, date)
				} else {
					text += fmt.Sprintf("%s%d %s — %s (%s)\n", sign, abs(s.Points), s.CategoryLabel, reason, date)
				}

				count++
				if count >= 10 {
					break
				}
			}
		} else {
			text += "\n\n📖 История: пусто"
		}
	}

	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, text))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func HandleParentRatingRequest(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, parentID int64) {
	children, err := db.GetChildrenByParentID(database, parentID)
	if err != nil || len(children) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "У вас нет привязанных детей."))
		return
	}

	if len(children) == 1 {
		ShowStudentRating(bot, database, chatID, children[0].ID)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, child := range children {
		text := fmt.Sprintf("%s (%d%s класс)", child.Name, *child.ClassNumber, *child.ClassLetter)
		callback := fmt.Sprintf("show_rating_student_%d", child.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(text, callback),
		))
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, "Выберите ребёнка для просмотра рейтинга:")
	msg.ReplyMarkup = markup
	bot.Send(msg)
}

func ShowStudentRating(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, studentID int64) {
	// Получаем категории и суммы
	rows, err := database.Query(`
		SELECT c.label, SUM(s.points) as total
		FROM scores s
		JOIN categories c ON s.category_id = c.id
		WHERE s.student_id = $1 AND s.status = 'approved'
		GROUP BY s.category_id
	`, studentID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при получении рейтинга."))
		return
	}
	defer rows.Close()

	summary := make(map[string]int)
	total := 0

	for rows.Next() {
		var label string
		var sum int
		if err := rows.Scan(&label, &sum); err != nil {
			continue
		}
		summary[label] = sum
		total += sum
	}

	text := fmt.Sprintf("📊 Общий рейтинг: %d баллов\n\n", total)
	for label, val := range summary {
		text += fmt.Sprintf("▫️ %s: %d\n", label, val)
	}

	history, err := db.GetScoreByStudent(database, studentID)
	if err == nil && len(history) > 0 {
		text += "\n\n📖 История:\n"
		count := 0
		for _, s := range history {
			if s.Status != "approved" {
				continue
			}
			sign := "+"
			if s.Type == "remove" {
				sign = "-"
			}
			date := s.CreatedAt.Format("02.01.2006")
			reason := "-"
			if s.Comment != nil && *s.Comment != "" {
				reason = *s.Comment
			}
			if reason == "-" {
				text += fmt.Sprintf("%s%d %s (%s)\n", sign, abs(s.Points), s.CategoryLabel, date)
			} else {
				text += fmt.Sprintf("%s%d %s — %s (%s)\n", sign, abs(s.Points), s.CategoryLabel, reason, date)
			}
			count++
			if count >= 10 {
				break
			}
		}
	} else {
		text += "\n\n📖 История: пусто"
	}

	bot.Send(tgbotapi.NewMessage(chatID, text))
}
