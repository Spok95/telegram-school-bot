package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
)

func HandleMyScore(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	telegramID := msg.From.ID
	user, err := db.GetUserByTelegramID(database, telegramID)
	if err != nil {
		log.Println("Ошибка при получении пользователя:", err)
		sendText(bot, msg.Chat.ID, "❌ Пользователь не найден.")
		return
	}

	var targetID int64
	// Если родитель — найдём ребёнка
	if user.Role != nil && *user.Role == "parent" {
		err = database.QueryRow(`SELECT child_id FROM users WHERE id = ? AND role = 'parent'`, user.ID).Scan(&targetID)
		if err != nil {
			sendText(bot, msg.Chat.ID, "❌ Не удалось найти привязанного ученика.")
			return
		}
	} else {
		targetID = user.ID
	}

	// Получаем сумму баллов по категориям
	rows, err := database.Query(`
SELECT c.label, SUM(points) as total
FROM scores s
JOIN categories c ON s.category_id = c.id
WHERE s.student_id = ? AND s.approved = 1
GROUP BY s.category_id
`, telegramID)
	if err != nil {
		log.Println("Ошибка при подсчёте баллов:", err)
		sendText(bot, msg.Chat.ID, "❌ Ошибка при подсчёте баллов.")
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
	history, err := db.GetScoreByStudent(database, telegramID)
	if err != nil {
		log.Println("ошибка при получении истории:", err)
	} else {
		if len(history) > 0 {
			text += "\n\n📖 История:\n"
			count := 0
			for _, s := range history {
				if !s.Approved {
					continue
				}
				sign := "+"
				if s.Points < 0 {
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
