package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// StartStudentHistoryExcel Ученик → Excel история за активный период
func StartStudentHistoryExcel(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Student {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Доступно только ученикам.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if !u.IsActive {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Доступ к боту временно закрыт.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	act, err := db.GetActivePeriod(ctx, database)
	if err != nil || act == nil {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Активный период не найден.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	go generateAndSendStudentHistoryExcel(ctx, bot, database, chatID, u.ID, int(*u.ClassNumber), *u.ClassLetter, act.ID, act.Name)
}

// StartParentHistoryExcel Родитель → один ребёнок: сразу отчёт; несколько — выбор ребёнка
func StartParentHistoryExcel(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, err := db.GetUserByTelegramID(ctx, database, chatID)
	if err != nil || u == nil || u.Role == nil || *u.Role != models.Parent {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Доступно только родителям.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	children, err := db.GetChildrenByParentID(ctx, database, u.ID)
	if err != nil || len(children) == 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "У вас нет привязанных активных детей.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if len(children) == 1 {
		if act, _ := db.GetActivePeriod(ctx, database); act != nil {
			c := children[0]
			go generateAndSendStudentHistoryExcel(ctx, bot, database, chatID, c.ID, int(*c.ClassNumber), *c.ClassLetter, act.ID, act.Name)
			return
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Активный период не найден.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
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
	if _, err := tg.Send(bot, msgOut); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// HandleHistoryExcelCallback Callback выбора ребёнка
func HandleHistoryExcelCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data
	if _, err := tg.Request(bot, tgbotapi.NewCallback(cb.ID, "")); err != nil {
		metrics.HandlerErrors.Inc()
	}

	if strings.HasPrefix(data, "hist_excel_student_") {
		idStr := strings.TrimPrefix(data, "hist_excel_student_")
		stuID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Ошибка: не удалось определить ребёнка.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		u, err := db.GetUserByID(ctx, database, stuID)
		if err != nil || u.ID == 0 || u.ClassNumber == nil || u.ClassLetter == nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось определить класс ученика.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		if act, _ := db.GetActivePeriod(ctx, database); act != nil {
			go generateAndSendStudentHistoryExcel(ctx, bot, database, chatID, stuID, int(*u.ClassNumber), *u.ClassLetter, act.ID, act.Name)
			return
		}
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Активный период не найден.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

// generateAndSendStudentHistoryExcel Реальная генерация Excel: используем готовый generateStudentReport(...)
func generateAndSendStudentHistoryExcel(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, studentID int64, classNumber int, classLetter string, periodID int64, periodName string) {
	scores, err := db.GetScoresByStudentAndPeriod(ctx, database, studentID, int(periodID))
	if err != nil {
		log.Println("history export: get scores:", err)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось получить историю за период.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	collective := calcCollectiveForClassPeriod(ctx, database, int64(classNumber), classLetter, periodID) // без «Аукцион»
	className := fmt.Sprintf("%d%s", classNumber, classLetter)

	filePath, err := generateStudentReport(scores, collective, className, periodName)
	if err != nil {
		log.Println("history export: generate file:", err)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Не удалось сформировать Excel-файл.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	doc.Caption = fmt.Sprintf("📊 История по ученику за период: %s", periodName)
	if _, err := tg.Send(bot, doc); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// calcCollectiveForClassPeriod Коллективный рейтинг класса за период (30%), исключая категорию «Аукцион».
func calcCollectiveForClassPeriod(ctx context.Context, database *sql.DB, classNumber int64, classLetter string, periodID int64) int64 {
	classScores, err := db.GetScoresByClassAndPeriod(ctx, database, classNumber, classLetter, periodID)
	if err != nil {
		return 0
	}
	auctionID := db.GetCategoryIDByName(ctx, database, "Аукцион")
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
