package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

// стартовое меню (новое сообщение)
func StartExportFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, _ := db.GetUserByTelegramID(database, chatID)
	if u == nil || !fsmutil.MustBeActiveForOps(u) {
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Доступ временно закрыт. Обратитесь к администратору."))
		return
	}
	exportStates[chatID] = &ExportFSMState{Step: ExportStepReportType}

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По ученику", "export_type_student"),
			tgbotapi.NewInlineKeyboardButtonData("По классу", "export_type_class"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По школе", "export_type_school"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Пользователи", "exp_users_open"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "export_cancel"),
		),
	}
	msgOut := tgbotapi.NewMessage(chatID, "📊 Выберите тип отчёта:")
	msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	bot.Send(msgOut)
}

func HandleExportCallback(bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state, ok := exportStates[chatID]
	if !ok {
		return
	}
	data := cq.Data

	// ❌ Отмена — прячем клаву и меняем текст у ЭТОГО же сообщения
	if data == "export_cancel" {
		delete(exportStates, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Экспорт отменён.")
		bot.Send(edit)
		return
	}

	// ⬅️ Назад — редактируем текущее сообщение на предыдущий шаг
	if data == "export_back" {
		switch state.Step {
		case ExportStepPeriodMode:
			state.Step = ExportStepReportType
			editMenu(bot, chatID, cq.Message.MessageID, "📊 Выберите тип отчёта:", startRows())
			return
		case ExportStepFixedPeriodSelect:
			state.Step = ExportStepPeriodMode
			editMenu(bot, chatID, cq.Message.MessageID, "📅 Выберите режим периода:", periodModeRows())
			return
		case ExportStepClassNumber:
			state.Step = ExportStepPeriodMode
			editMenu(bot, chatID, cq.Message.MessageID, "📅 Выберите режим периода:", periodModeRows())
			return
		case ExportStepClassLetter:
			state.Step = ExportStepClassNumber
			editMenu(bot, chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", classNumberRows("export_class_number_"))
			return
		case ExportStepStudentSelect:
			state.Step = ExportStepClassLetter
			editMenu(bot, chatID, cq.Message.MessageID, "🔠 Выберите букву класса:", classLetterRows("export_class_letter_"))
			return
		case ExportStepCustomStartDate:
			// Назад со старта → к выбору режима периода
			state.Step = ExportStepPeriodMode
			editMenu(bot, chatID, cq.Message.MessageID, "📅 Выберите режим периода:", periodModeRows())
			return
		case ExportStepCustomEndDate:
			// Назад с конца → обратно к вводу старт‑даты
			state.Step = ExportStepCustomStartDate
			rows := [][]tgbotapi.InlineKeyboardButton{
				fsmutil.BackCancelRow("export_back", "export_cancel"),
			}
			cfg := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "📆 Введите дату начала (ДД.ММ.ГГГГ):")
			mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
			cfg.ReplyMarkup = &mk
			bot.Send(cfg)
			return
		default:
			delete(exportStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Экспорт отменён.")
			bot.Send(edit)
			return
		}
	}

	switch state.Step {
	case ExportStepReportType:
		if strings.HasPrefix(data, "export_type_") {
			state.ReportType = strings.TrimPrefix(data, "export_type_")
			state.Step = ExportStepPeriodMode
			editMenu(bot, chatID, cq.Message.MessageID, "📅 Выберите режим периода:", periodModeRows())
		}

	case ExportStepPeriodMode:
		if data == "export_mode_fixed" {
			state.PeriodMode = "fixed"
			state.Step = ExportStepFixedPeriodSelect
			_ = db.SetActivePeriod(database)
			periods, err := db.ListPeriods(database)
			if err != nil || len(periods) == 0 {
				delete(exportStates, chatID)
				edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ Не удалось загрузить периоды.")
				bot.Send(edit)
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
			rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
			editMenu(bot, chatID, cq.Message.MessageID, "📘 Выберите учебный период:", rows)

		} else if data == "export_mode_custom" {
			state.PeriodMode = "custom"
			state.Step = ExportStepCustomStartDate

			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)

			rows := [][]tgbotapi.InlineKeyboardButton{
				fsmutil.BackCancelRow("export_back", "export_cancel"),
			}
			msg := tgbotapi.NewMessage(chatID, "📆 Введите дату начала (ДД.ММ.ГГГГ):")
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
			// для текстовых шагов неизбежно создаём новое сообщение
			bot.Send(msg)
		}

	case ExportStepFixedPeriodSelect:
		if strings.HasPrefix(data, "export_period_") {
			idStr := strings.TrimPrefix(data, "export_period_")
			id, _ := strconv.ParseInt(idStr, 10, 64)
			state.PeriodID = &id

			// дальше — в зависимости от типа отчёта
			if state.ReportType == "school" {
				go generateExportReport(bot, database, chatID, state)
				delete(exportStates, chatID)
				return
			}
			// student / class → выбор номера класса (редактирование)
			state.Step = ExportStepClassNumber
			editMenu(bot, chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", classNumberRows("export_class_number_"))
		}

	case ExportStepClassNumber:
		if strings.HasPrefix(data, "export_class_number_") {
			state.ClassNumber, _ = strconv.ParseInt(strings.TrimPrefix(data, "export_class_number_"), 10, 64)
			state.Step = ExportStepClassLetter
			editMenu(bot, chatID, cq.Message.MessageID, "🔠 Выберите букву класса:", classLetterRows("export_class_letter_"))
		}

	case ExportStepClassLetter:
		if strings.HasPrefix(data, "export_class_letter_") {
			state.ClassLetter = strings.TrimPrefix(data, "export_class_letter_")
			if state.ReportType == "student" {
				state.Step = ExportStepStudentSelect
				// тут нам важно оставить тот же message_id, поэтому редактируем только клавиатуру
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

	// текстовая отмена
	if fsmutil.IsCancelText(msg.Text) {
		delete(exportStates, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Экспорт отменён."))
		return
	}

	switch state.Step {
	case ExportStepCustomStartDate:
		date, err := time.Parse("02.01.2006", strings.TrimSpace(msg.Text))
		if err != nil {
			rows := [][]tgbotapi.InlineKeyboardButton{
				fsmutil.BackCancelRow("export_back", "export_cancel"),
			}
			msg := tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату в формате ДД.ММ.ГГГГ.")
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
			bot.Send(msg)
			return
		}
		state.FromDate = &date
		state.Step = ExportStepCustomEndDate
		rows := [][]tgbotapi.InlineKeyboardButton{
			fsmutil.BackCancelRow("export_back", "export_cancel"),
		}
		msg := tgbotapi.NewMessage(chatID, "📅 Введите дату окончания (ДД.ММ.ГГГГ):")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		bot.Send(msg)

	case ExportStepCustomEndDate:
		date, err := time.Parse("02.01.2006", strings.TrimSpace(msg.Text))
		if err != nil {
			rows := [][]tgbotapi.InlineKeyboardButton{
				fsmutil.BackCancelRow("export_back", "export_cancel"),
			}
			msg := tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату в формате ДД.ММ.ГГГГ.")
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
			bot.Send(msg)
			return
		}
		endOfDay := date.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		state.ToDate = &endOfDay

		// дальше как после выбора периода
		if state.ReportType == "school" {
			go generateExportReport(bot, database, chatID, state)
			delete(exportStates, chatID)
			return
		}
		state.Step = ExportStepClassNumber
		// после текстового шага нет message_id для редактирования — отправляем новое меню
		msgOut := tgbotapi.NewMessage(chatID, "🔢 Выберите номер класса:")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(classNumberRows("export_class_number_")...)
		bot.Send(msgOut)
	}
}

// ==== вспомогательные меню (редактирование текущего сообщения) ====

func startRows() [][]tgbotapi.InlineKeyboardButton {
	return [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По ученику", "export_type_student"),
			tgbotapi.NewInlineKeyboardButtonData("По классу", "export_type_class"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("По школе", "export_type_school"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Пользователи", "exp_users_open"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "export_cancel"),
		),
	}
}

func periodModeRows() [][]tgbotapi.InlineKeyboardButton {
	return [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📆 Установленный", "export_mode_fixed"),
			tgbotapi.NewInlineKeyboardButtonData("🗓 Произвольный", "export_mode_custom"),
		),
		fsmutil.BackCancelRow("export_back", "export_cancel"),
	}
}

func classNumberRows(prefix string) [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		btn := tgbotapi.NewInlineKeyboardButtonData(strconv.Itoa(i), fmt.Sprintf("%s%d", prefix, i))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}
	rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
	return rows
}

func classLetterRows(prefix string) [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(l, prefix+l)))
	}
	rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
	return rows
}

// Единый редактор текста + клавиатуры
func editMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	bot.Send(cfg)
}

// Выбор студентов — редактируем только клавиатуру у текущего сообщения
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
	rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, tgbotapi.NewInlineKeyboardMarkup(rows...))
	bot.Send(edit)
}

func generateExportReport(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, state *ExportFSMState) {
	// защита от двойного запуска
	key := fmt.Sprintf("export:%d:%s", chatID, state.ReportType)
	if !fsmutil.SetPending(chatID, key) {
		bot.Send(tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…"))
		return
	}
	defer fsmutil.ClearPending(chatID, key)

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
					part, err := db.GetScoresByStudentAndPeriod(database, id, int(*state.PeriodID))
					if err != nil {
						log.Println("Ошибка при получении баллов:", err)
					}
					scores = append(scores, part...)
				}
			} else if state.ReportType == "class" {
				scores, err = db.GetScoresByClassAndPeriod(database, state.ClassNumber, state.ClassLetter, *state.PeriodID)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			} else if state.ReportType == "school" {
				scores, err = db.GetScoresByPeriod(database, int(*state.PeriodID))
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			}
		case "custom":
			if state.FromDate == nil || state.ToDate == nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Даты не заданы"))
				return
			}
			periodLabel = fmt.Sprintf("%s–%s", state.FromDate.Format("02.01.2006"), state.ToDate.Format("02.01.2006"))
			if state.ReportType == "student" {
				for _, id := range state.SelectedStudentIDs {
					part, err := db.GetScoresByStudentAndDateRange(database, id, *state.FromDate, *state.ToDate)
					if err != nil {
						log.Println("Ошибка при получении баллов:", err)
					}
					scores = append(scores, part...)
				}
			} else if state.ReportType == "class" {
				scores, err = db.GetScoresByClassAndDateRange(database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			} else if state.ReportType == "school" {
				scores, err = db.GetScoresByDateRange(database, *state.FromDate, *state.ToDate)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			}
		}

		var filePath string
		var collective int64
		var className string

		// --- Вычисляем коллективный рейтинг ---
		// Для отчёта по классу
		if state.ReportType == "class" && len(scores) > 0 {
			className = fmt.Sprintf("%d%s", int(state.ClassNumber), state.ClassLetter)
			if state.PeriodID != nil {
				if classScores, err2 := db.GetScoresByClassAndPeriod(database, state.ClassNumber, state.ClassLetter, *state.PeriodID); err2 == nil {
					stu := map[int64]int{}
					for _, sc := range classScores {
						stu[sc.StudentID] += sc.Points
					}
					for _, tot := range stu {
						collective += int64((tot * 30) / 100)
					}
				}
			} else if state.FromDate != nil && state.ToDate != nil {
				if classScores, err2 := db.GetScoresByClassAndDateRange(database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate); err2 == nil {
					stu := map[int64]int{}
					for _, sc := range classScores {
						stu[sc.StudentID] += sc.Points
					}
					for _, tot := range stu {
						collective += int64((tot * 30) / 100)
					}
				}
			}
		}
		// Для отчёта по ученику — класс берём из выбранного состояния (учеников выбираем внутри класса)
		if state.ReportType == "student" && len(scores) > 0 {
			className = fmt.Sprintf("%d%s", int(state.ClassNumber), state.ClassLetter)
			if state.PeriodID != nil {
				if classScores, err2 := db.GetScoresByClassAndPeriod(database, state.ClassNumber, state.ClassLetter, *state.PeriodID); err2 == nil {
					stu := map[int64]int{}
					for _, sc := range classScores {
						stu[sc.StudentID] += sc.Points
					}
					for _, tot := range stu {
						collective += int64((tot * 30) / 100)
					}
				}
			} else if state.FromDate != nil && state.ToDate != nil {
				if classScores, err2 := db.GetScoresByClassAndDateRange(database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate); err2 == nil {
					stu := map[int64]int{}
					for _, sc := range classScores {
						stu[sc.StudentID] += sc.Points
					}
					for _, tot := range stu {
						collective += int64((tot * 30) / 100)
					}
				}
			}
		}
		switch state.ReportType {
		case "student":
			filePath, err = generateStudentReport(scores, collective, className, periodLabel)
		case "class":
			filePath, err = generateClassReport(scores, collective, className, periodLabel)
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

func GetExportState(userID int64) *ExportFSMState {
	return exportStates[userID]
}
func ClearExportState(userID int64) {
	delete(exportStates, userID)
}
