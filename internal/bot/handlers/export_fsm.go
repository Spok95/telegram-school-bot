package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type ExportFSMState struct {
	Step       int
	ReportType string
	PeriodID   int64
}

var exportStates = make(map[int64]*ExportFSMState)

func StartExportFSM(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	exportStates[chatID] = &ExportFSMState{Step: 1}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По ученику", "export_type_student"),
			tgbotapi.NewInlineKeyboardButtonData("По классу", "export_type_class"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По школе", "export_type_school"),
		),
	)

	log.Println("📥 Старт FSM экспорта")

	msgOut := tgbotapi.NewMessage(chatID, "📊 Выберите тип отчёта:")
	msgOut.ReplyMarkup = keyboard
	bot.Send(msgOut)
}

func HandleExportCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state, ok := exportStates[chatID]
	if !ok {
		log.Println("⚠️ Нет состояния FSM для chatID:", chatID)
		return
	}

	data := cq.Data

	log.Println("➡️ Получен callback:", data)

	if data == "export_type_student" || data == "export_type_class" || data == "export_type_school" {
		state.ReportType = data[len("export_type_"):]
		state.Step = 2

		// Показываем периоды
		periods, err := db.ListPeriods(database)
		if err != nil || len(periods) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить периоды"))
			delete(exportStates, chatID)
			return
		}
		var buttons [][]tgbotapi.InlineKeyboardButton
		for _, p := range periods {
			label := p.Name
			if p.IsActive {
				label += " ✅"
			}
			callback := "export_period_" + strconv.FormatInt(p.ID, 10)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, callback)))
		}
		bot.Send(tgbotapi.NewMessage(chatID, "📆 Выберите учебный период:"))
		bot.Send(tgbotapi.NewMessage(chatID, "👇"))
		msg := tgbotapi.NewMessage(chatID, "Выберите период:")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
		bot.Send(msg)
		return
	}
	if state.Step == 2 && data[:14] == "export_period_" {
		idStr := data[len("export_period_"):]
		id, _ := strconv.ParseInt(idStr, 10, 64)
		state.PeriodID = id
		state.Step = 3

		bot.Send(tgbotapi.NewMessage(chatID, "⏳ Формирую Excel-файл..."))

		// Вызов генерации Excel-файла
		go func() {
			err := GenerateReport(bot, database, chatID, state.ReportType, state.PeriodID)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при экспорте: "+err.Error()))
			}
		}()

		delete(exportStates, chatID)
	}
}

func generateExport(scores []models.ScoreWithUser) (string, error) {
	f := excelize.NewFile()
	sheet := "Report"
	f.SetSheetName("Sheet1", sheet)

	// Заголовки
	headers := []string{"ФИО ученика", "Класс", "Категория", "Баллы", "Комментарий", "Кто добавил", "Дата добавления"}
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		f.SetCellValue("Sheet1", cell, h)
	}
	// Данные
	for i, s := range scores {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), s.StudentName)
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("%d%s", s.ClassNumber, s.ClassLetter))
		_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", row), s.CategoryLabel)
		_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", row), s.Points)
		_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", row), s.Comment)
		_ = f.SetCellValue(sheet, fmt.Sprintf("F%d", row), s.AddedByName)
		_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", row), s.CreatedAt.Format("2006-01-02 15:04"))
	}

	// Автосохранение
	filename := fmt.Sprintf("report_%d.xlsx", time.Now().Unix())
	path := filepath.Join(os.TempDir(), filename)

	if err := f.SaveAs(path); err != nil {
		return "", err
	}
	return path, nil
}

func GenerateReport(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, exportType string, periodID int64) error {
	scores, err := db.GetScoresByPeriod(database, int(periodID))
	if err != nil {
		return fmt.Errorf("ошибка получения баллов: %w", err)
	}

	period, err := db.GetPeriodByID(database, int(periodID))
	if err != nil {
		return fmt.Errorf("ошибка получения периода: %w", err)
	}

	filePath, err := generateExport(scores)
	if err != nil {
		return fmt.Errorf("ошибка создания Excel файла: %w", err)
	}

	// Отправка Excel файла пользователю
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	doc.Caption = fmt.Sprintf("📊 Отчёт за период: %s", period.Name)

	if _, err := bot.Send(doc); err != nil {
		return fmt.Errorf("ошибка отправки файла: %w", err)
	}
	return nil
}
