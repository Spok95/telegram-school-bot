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
	var userID int64
	var role string
	var targetID int64

	// Получаем ID и роль
	err := database.QueryRow(`SELECT id, role FROM users WHERE telegram_id = ?`, telegramID).Scan(&userID, &role)
	if err != nil {
		log.Println("Ошибка поиска пользователя:", err)
		sendText(bot, msg.Chat.ID, "❌ Ошибка при получении данных.")
		return
	}

	// Если родитель — найдём ребёнка
	if role == "parent" {
		err := database.QueryRow(`SELECT child_id FROM users WHERE id = ? AND role = 'parent'`, userID).Scan(&targetID)
		if err != nil {
			sendText(bot, msg.Chat.ID, "❌ Не удалось найти привязанного ученика.")
			return
		}
	} else {
		targetID = userID
	}

	// Получаем сумму баллов по категориям
	rows, err := database.Query(`
SELECT category, SUM(points) as total
FROM scores
WHERE student_id = ? AND approved = 1
GROUP BY category
`, targetID)
	if err != nil {
		log.Println("Ошибка при подсчёте баллов:", err)
		sendText(bot, msg.Chat.ID, "❌ Ошибка при подсчёте баллов.")
		return
	}
	defer rows.Close()

	summary := make(map[string]int)
	total := 0

	for rows.Next() {
		var cat string
		var sum int
		if err := rows.Scan(&cat, &sum); err != nil {
			continue
		}
		summary[cat] = sum
		total += sum
	}

	// Формируем текст ответа
	text := fmt.Sprintf("📊 Ваш общий рейтинг: %d баллов\n\n", total)
	for _, cat := range []string{"work", "elective", "activity", "social", "duty"} {
		val := summary[cat]
		text += fmt.Sprintf("▫️ %s: %d\n", categoryToLabel(cat), val)
	}

	// Получаем все начисления/списания
	history, err := db.GetScoreByStudent(database, userID)
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
				var reason string
				if s.Comment != nil && *s.Comment != "" {
					reason = *s.Comment
				} else {
					reason = "-"
				}

				if reason == "-" {
					text += fmt.Sprintf("%s%d %s (%s)\n", sign, abs(s.Points), categoryToLabel(s.Category), date)
				} else {
					text += fmt.Sprintf("%s%d %s — %s (%s)\n", sign, abs(s.Points), categoryToLabel(s.Category), reason, date)
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

func categoryToLabel(key string) string {
	switch key {
	case "work":
		return "Работа на уроке"
	case "elective":
		return "Курсы по выбору"
	case "activity":
		return "Внеурочная активность"
	case "social":
		return "Социальные поступки"
	case "duty":
		return "Дежурство"
	default:
		return key
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
