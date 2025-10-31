package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleMyScore(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	user, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil {
		log.Println("Пользователь найден:", err)
	}
	if user == nil || !user.IsActive {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт. Обратитесь к администратору.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	targetID := user.ID

	// Если родитель — ищем telegram_id ребёнка
	if *user.Role == models.Parent {
		var studentInternalID int64
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err := database.QueryRowContext(ctx, `
			SELECT student_id FROM parents_students WHERE parent_id = $1
		`, user.ID).Scan(&studentInternalID)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось найти привязанного ученика.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		targetID = studentInternalID
	}

	// Границы текущего учебного года [from, to)
	now := time.Now()
	from, to := db.SchoolYearBounds(now)
	yearLabel := db.SchoolYearLabel(db.CurrentSchoolYearStartYear(now))

	// Получаем категории и суммы ТОЛЬКО за текущий учебный год
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := database.QueryContext(ctx, `
		SELECT c.label, SUM(s.points) AS total
		FROM scores s
		JOIN categories c ON s.category_id = c.id
		WHERE s.student_id = $1 
		  AND s.status = 'approved'
		  AND s.created_at >= $2 AND s.created_at < $3
		GROUP BY c.label
		ORDER BY total DESC
	`, targetID, from, to)
	if err != nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Ошибка при получении рейтинга.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { _ = rows.Close() }()

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
	text := fmt.Sprintf("🎓 Учебный год: %s\n📊 Ваш общий рейтинг: %d баллов\n\n", yearLabel, total)
	for label, val := range summary {
		text += fmt.Sprintf("▫️ %s: %d\n", label, val)
	}

	// Получаем все начисления/списания
	history, err := db.GetScoresByStudentAndDateRange(ctx, database, targetID, from, to)
	if err != nil {
		log.Println("ошибка при получении истории:", err)
	} else {
		if len(history) > 0 {
			text += "\n\n📖 История начислений:\n"
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
			text += "\n\n📖 История начислений: пусто"
		}
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, text)); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func HandleParentRatingRequest(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, parentID int64) {
	children, err := db.GetChildrenByParentID(ctx, database, parentID)
	if err != nil || len(children) == 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "У вас нет привязанных детей.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	if len(children) == 1 {
		ShowStudentRating(ctx, bot, database, chatID, children[0].ID)
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
	if _, err := tg.Send(bot, msg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func ShowStudentRating(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, studentID int64) {
	// Границы текущего учебного года
	now := time.Now()
	from, to := db.SchoolYearBounds(now)
	yearLabel := db.SchoolYearLabel(db.CurrentSchoolYearStartYear(now))

	// ФИО ребёнка для заголовка
	childName := ""
	if u, err := db.GetUserByID(ctx, database, studentID); err == nil && u.Name != "" {
		childName = u.Name
	}

	// Суммы только за текущий учебный год
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := database.QueryContext(ctx, `
		SELECT c.label, SUM(s.points) AS total
		FROM scores s
		JOIN categories c ON s.category_id = c.id
		WHERE s.student_id = $1 
		  AND s.status = 'approved'
		  AND s.created_at >= $2 AND s.created_at < $3
		GROUP BY c.label
		ORDER BY total DESC
	`, studentID, from, to)
	if err != nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Ошибка при получении рейтинга.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer func() { _ = rows.Close() }()

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

	header := fmt.Sprintf("🎓 Учебный год: %s\n", yearLabel)
	if childName != "" {
		header += fmt.Sprintf("👤 Ребёнок: %s\n", childName)
	}
	text := fmt.Sprintf("%s📊 Общий рейтинг: %d баллов\n\n", header, total)

	for label, val := range summary {
		text += fmt.Sprintf("▫️ %s: %d\n", label, val)
	}

	history, err := db.GetScoresByStudentAndDateRange(ctx, database, studentID, from, to)
	if err == nil && len(history) > 0 {
		text += "\n\n📖 История начислений:\n"
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
		text += "\n\n📖 История начислений: пусто"
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, text)); err != nil {
		metrics.HandlerErrors.Inc()
	}
}
