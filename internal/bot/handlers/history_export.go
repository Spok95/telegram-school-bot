package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Ученик → Excel история за активный период
func StartStudentHistoryExcel(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, err := db.GetUserByTelegramID(database, chatID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Student {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Доступно только ученикам."))
		return
	}
	if !u.IsActive {
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт."))
		return
	}
	act, err := db.GetActivePeriod(database)
	if err != nil || act == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Активный период не найден."))
		return
	}
	go generateAndSendStudentHistoryExcel(bot, database, chatID, u.ID, int(*u.ClassNumber), *u.ClassLetter, act.ID, act.Name)
}

// Родитель → один ребёнок: сразу отчёт; несколько — выбор ребёнка
func StartParentHistoryExcel(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, err := db.GetUserByTelegramID(database, chatID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Parent {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Доступно только родителям."))
		return
	}
	children, err := db.GetChildrenByParentID(database, u.ID)
	if err != nil || len(children) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "У вас нет привязанных активных детей."))
		return
	}
	if len(children) == 1 {
		if act, _ := db.GetActivePeriod(database); act != nil {
			c := children[0]
			go generateAndSendStudentHistoryExcel(bot, database, chatID, c.ID, int(*c.ClassNumber), *c.ClassLetter, act.ID, act.Name)
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Активный период не найден."))
		return
	}
	// Выбор ребёнка
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range children {
		label := fmt.Sprintf("%s (%d%s)", c.Name, *c.ClassNumber, *c.ClassLetter)
		cb := fmt.Sprintf("hist_excel_student_%d", c.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, cb)))
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msgOut := tgbotapi.NewMessage(chatID, "Выберите ребёнка для отчёта за текущий период:")
	msgOut.ReplyMarkup = markup
	bot.Send(msgOut)
}

// Callback выбора ребёнка
func HandleHistoryExcelCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data
	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	if strings.HasPrefix(data, "hist_excel_student_") {
		idStr := strings.TrimPrefix(data, "hist_excel_student_")
		stuID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка: не удалось определить ребёнка."))
			return
		}
		u, err := db.GetUserByID(database, stuID)
		if err != nil || u.ID == 0 || u.ClassNumber == nil || u.ClassLetter == nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось определить класс ученика."))
			return
		}
		if act, _ := db.GetActivePeriod(database); act != nil {
			go generateAndSendStudentHistoryExcel(bot, database, chatID, stuID, int(*u.ClassNumber), *u.ClassLetter, act.ID, act.Name)
			return
		}
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Активный период не найден."))
	}
}

// Реальная генерация Excel: используем готовый generateStudentReport(...)
func generateAndSendStudentHistoryExcel(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, studentID int64, classNumber int, classLetter string, periodID int64, periodName string) {
	scores, err := db.GetScoresByStudentAndPeriod(database, studentID, int(periodID))
	if err != nil {
		log.Println("history export: get scores:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось получить историю за период."))
		return
	}
	collective := calcCollectiveForClassPeriod(database, int64(classNumber), classLetter, periodID) // без «Аукцион»
	className := fmt.Sprintf("%d%s", classNumber, classLetter)

	filePath, err := generateStudentReport(scores, collective, className, periodName)
	if err != nil {
		log.Println("history export: generate file:", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось сформировать Excel-файл."))
		return
	}
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	doc.Caption = fmt.Sprintf("📊 История по ученику за период: %s", periodName)
	bot.Send(doc)
}

// Коллективный рейтинг класса за период (30%), исключая категорию «Аукцион».
func calcCollectiveForClassPeriod(database *sql.DB, classNumber int64, classLetter string, periodID int64) int64 {
	classScores, err := db.GetScoresByClassAndPeriod(database, classNumber, classLetter, periodID)
	if err != nil {
		return 0
	}
	auctionID := db.GetCategoryIDByName(database, "Аукцион")
	totals := map[int64]int{}
	for _, sc := range classScores {
		if int(sc.CategoryID) == auctionID {
			continue
		}
		totals[sc.StudentID] += sc.Points
	}
	var collective int64 = 0
	for _, t := range totals {
		collective += int64((t * 30) / 100)
	}
	return collective
}
