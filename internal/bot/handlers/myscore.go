package handlers

import (
	"database/sql"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
)

func HandleMyScore(bot *tgbotapi.BotAPI, db *sql.DB, msg *tgbotapi.Message) {
	telegramID := msg.From.ID
	var userID int64
	var role string
	var targetID int64

	// Получаем ID и роль
	err := db.QueryRow(`SELECT id, role FROM users WHERE telegram_id = ?`, telegramID).Scan(&userID, &role)
	if err != nil {
		log.Println("Ошибка поиска пользователя:", err)
		sendText(bot, msg.Chat.ID, "❌ Ошибка при получении данных.")
		return
	}

	// Если родитель — найдём ребёнка
	if role == "parent" {
		err := db.QueryRow(`SELECT id FROM users WHERE parent_id = ? AND role = 'student'`, userID).Scan(&targetID)
		if err != nil {
			sendText(bot, msg.Chat.ID, "❌ Не удалось найти привязанного ученика.")
			return
		}
	} else {
		targetID = userID
	}

	// Получаем сумму баллов по категориям
	rows, err := db.Query(`
SELECT category, SUM(value) as total
FROM scores
WHERE user_id = ? AND approved = 1
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
