package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"strconv"
	"strings"
	"time"
)

const (
	ExportStepReportType = iota
	ExportStepPeriodMode
	ExportStepFixedPeriodSelect
	ExportStepCustomStartDate
	ExportStepCustomEndDate
	ExportStepClassNumber
	ExportStepClassLetter
	ExportStepStudentSelect
)

type ExportFSMState struct {
	Step               int
	ReportType         string
	PeriodMode         string
	PeriodID           *int64
	FromDate           *time.Time
	ToDate             *time.Time
	ClassNumber        int64
	ClassLetter        string
	SelectedStudentIDs []int64
}

var exportStates = make(map[int64]*ExportFSMState)

func StartExportFSM(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	exportStates[chatID] = &ExportFSMState{Step: ExportStepReportType}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По ученику", "export_type_student"),
			tgbotapi.NewInlineKeyboardButtonData("По классу", "export_type_class"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По школе", "export_type_school"),
		),
	)

	msgOut := tgbotapi.NewMessage(chatID, "📊 Выберите тип отчёта:")
	msgOut.ReplyMarkup = keyboard
	bot.Send(msgOut)
}

func HandleExportCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state, ok := exportStates[chatID]
	if !ok {
		return
	}
	data := cq.Data

	switch state.Step {
	case ExportStepReportType:
		if strings.HasPrefix(data, "export_type_") {
			typeVal := strings.TrimPrefix(data, "export_type_")
			state.ReportType = typeVal
			state.Step = ExportStepPeriodMode
			promptExportPeriodMode(bot, chatID)
		}
	case ExportStepPeriodMode:
		if data == "export_mode_fixed" {
			state.PeriodMode = "fixed"
			state.Step = ExportStepFixedPeriodSelect
			_ = db.SetActivePeriod(database)
			periods, err := db.ListPeriods(database)
			if err != nil || len(periods) == 0 {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить периоды"))
				delete(exportStates, chatID)
				return
			}
			var rows [][]tgbotapi.InlineKeyboardButton
			for _, p := range periods {
				label := p.Name
				if p.IsActive {
					label += " ✅"
				}
				cb := "export_period_" + strconv.FormatInt(p.ID, 10)
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, cb)))
			}
			msg := tgbotapi.NewMessage(chatID, "📘 Выберите учебный период:")
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
			bot.Send(msg)
		} else if data == "export_mode_custom" {
			state.PeriodMode = "custom"
			state.Step = ExportStepCustomStartDate
			bot.Send(tgbotapi.NewMessage(chatID, "📆 Введите дату начала (в формате ДД.ММ.ГГГГ):"))
		}
	case ExportStepFixedPeriodSelect:
		if strings.HasPrefix(data, "export_period_") {
			idStr := strings.TrimPrefix(data, "export_period_")
			id, _ := strconv.ParseInt(idStr, 10, 64)
			state.PeriodID = &id
			advanceExportStep(bot, database, chatID, state)
		}
	case ExportStepClassNumber:
		if strings.HasPrefix(data, "export_class_number_") {
			state.ClassNumber, _ = strconv.ParseInt(strings.TrimPrefix(data, "export_class_number_"), 10, 64)
			state.Step = ExportStepClassLetter
			promptClassLetterFSM(bot, chatID, "export_class_letter_")
		}
	case ExportStepClassLetter:
		if strings.HasPrefix(data, "export_class_letter_") {
			state.ClassLetter = strings.TrimPrefix(data, "export_class_letter_")
			if state.ReportType == "student" {
				state.Step = ExportStepStudentSelect
				promptStudentSelectExport(bot, database, cq)
			} else if state.ReportType == "class" {
				go generateExportReport(bot, database, chatID, state)
				delete(exportStates, chatID)
			}
		}
	case ExportStepStudentSelect:
		if strings.HasPrefix(data, "export_select_student_") {
			idStr := strings.TrimPrefix(data, "export_select_student_")
			id, _ := strconv.ParseInt(idStr, 10, 64)
			found := false
			for i, sid := range state.SelectedStudentIDs {
				if sid == id {
					state.SelectedStudentIDs = append(state.SelectedStudentIDs[:i], state.SelectedStudentIDs[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				state.SelectedStudentIDs = append(state.SelectedStudentIDs, id)
			}
			promptStudentSelectExport(bot, database, cq)
			return
		} else if data == "export_students_done" {
			if len(state.SelectedStudentIDs) == 0 {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Выберите хотя бы одного ученика."))
				return
			}
			bot.Request(tgbotapi.NewCallback(cq.ID, "📥 Отчёт формируется..."))
			go generateExportReport(bot, database, chatID, state)
			delete(exportStates, chatID)
		}
	}
}

func HandleExportText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := exportStates[chatID]
	if state == nil {
		return
	}

	text := msg.Text
	if state.Step == ExportStepCustomStartDate {
		date, err := time.Parse("02.01.2006", text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату в формате ДД.ММ.ГГГГ."))
			return
		}
		state.FromDate = &date
		state.Step = ExportStepCustomEndDate
		bot.Send(tgbotapi.NewMessage(chatID, "📅 Введите дату окончания (в формате ДД.ММ.ГГГГ.):"))
		return
	}

	if state.Step == ExportStepCustomEndDate {
		date, err := time.Parse("02.01.2006", text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите в формате ДД.ММ.ГГГГ."))
			return
		}
		state.ToDate = &date
		advanceExportStep(bot, database, chatID, state)
	}
}

func promptExportPeriodMode(bot *tgbotapi.BotAPI, chatID int64) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📆 Установленный", "export_mode_fixed"),
			tgbotapi.NewInlineKeyboardButtonData("🗓 Произвольный", "export_mode_custom"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "📅 Выберите режим периода:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func advanceExportStep(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, state *ExportFSMState) {
	switch state.ReportType {
	case "student":
		state.Step = ExportStepClassNumber
		promptClassNumberFSM(bot, chatID, "export_class_number_")
	case "class":
		state.Step = ExportStepClassNumber
		promptClassNumberFSM(bot, chatID, "export_class_number_")
	case "school":
		// запустить генерацию сразу, так как ничего выбирать не надо
		go generateExportReport(bot, database, chatID, state)
		delete(exportStates, chatID)
	}
}

func generateExportReport(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, state *ExportFSMState) {
	bot.Send(tgbotapi.NewMessage(chatID, "⏳ Формирую Excel-файл..."))
	go func() {
		var scores []models.ScoreWithUser
		var err error
		var periodLabel string

		switch state.PeriodMode {
		case "fixed":
			if state.PeriodID == nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Период не выбран"))
				return
			}
			p, _ := db.GetPeriodByID(database, int(*state.PeriodID))
			periodLabel = p.Name
			if state.ReportType == "student" {
				for _, id := range state.SelectedStudentIDs {
					part, _ := db.GetScoresByStudentAndPeriod(database, id, int(*state.PeriodID))
					scores = append(scores, part...)
				}
			} else if state.ReportType == "class" {
				scores, _ = db.GetScoresByClassAndPeriod(database, state.ClassNumber, state.ClassLetter, *state.PeriodID)
			} else if state.ReportType == "school" {
				scores, _ = db.GetScoresByPeriod(database, int(*state.PeriodID))
			}
		case "custom":
			if state.FromDate == nil || state.ToDate == nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Даты не заданы"))
				return
			}
			periodLabel = fmt.Sprintf("%s–%s", state.FromDate.Format("02.01.2006"), state.ToDate.Format("02.01.2006"))
			if state.ReportType == "student" {
				for _, id := range state.SelectedStudentIDs {
					part, _ := db.GetScoresByStudentAndDateRange(database, id, *state.FromDate, *state.ToDate)
					scores = append(scores, part...)
				}
			} else if state.ReportType == "class" {
				scores, _ = db.GetScoresByClassAndDateRange(database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate)
			} else if state.ReportType == "school" {
				scores, _ = db.GetScoresByDateRange(database, *state.FromDate, *state.ToDate)
			}
		}
		var filePath string
		switch state.ReportType {
		case "student":
			filePath, err = generateStudentReport(scores)
		case "class":
			filePath, err = generateClassReport(scores)
		case "school":
			filePath, err = generateSchoolReport(scores)
		}
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка генерации отчёта."))
			return
		}

		doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
		doc.Caption = fmt.Sprintf("📊 Отчёт за период: %s", periodLabel)
		bot.Send(doc)
	}()
}

func promptClassNumberFSM(bot *tgbotapi.BotAPI, chatID int64, prefix string) {
	msg := tgbotapi.NewMessage(chatID, "🔢 Выберите номер класса:")
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		btn := tgbotapi.NewInlineKeyboardButtonData(strconv.Itoa(i), fmt.Sprintf("%s%d", prefix, i))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msg)
}

func promptClassLetterFSM(bot *tgbotapi.BotAPI, chatID int64, prefix string) {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var row []tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(l, prefix+l))
	}
	msg := tgbotapi.NewMessage(chatID, "🔠 Выберите букву класса:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
	bot.Send(msg)
}

func promptStudentSelectExport(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state := exportStates[chatID]
	students, _ := db.GetStudentsByClass(database, state.ClassNumber, state.ClassLetter)
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, student := range students {
		selected := ""
		for _, id := range state.SelectedStudentIDs {
			if id == student.ID {
				selected = " ✅"
				break
			}
		}
		label := fmt.Sprintf("%s%s", student.Name, selected)
		cb := fmt.Sprintf("export_select_student_%d", student.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, cb)))
	}
	if len(state.SelectedStudentIDs) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Готово", "export_students_done")))
	}
	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, tgbotapi.NewInlineKeyboardMarkup(rows...))
	bot.Send(edit)

}

func GetExportState(userID int64) *ExportFSMState {
	return exportStates[userID]
}
