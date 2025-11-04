package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
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
	ExportStepSchoolYearSelect
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

func exportClassNumberRowsFromDB(ctx context.Context, database *sql.DB, prefix string) [][]tgbotapi.InlineKeyboardButton {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		return [][]tgbotapi.InlineKeyboardButton{
			fsmutil.BackCancelRow("export_back", "export_cancel"),
		}
	}

	numsSet := make(map[int]struct{})
	for _, c := range classes {
		numsSet[c.Number] = struct{}{}
	}
	var nums []int
	for n := range numsSet {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, n := range nums {
		cb := fmt.Sprintf("%s%d", prefix, n)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", n), cb),
		))
	}
	rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
	return rows
}

func exportClassLetterRowsFromDB(ctx context.Context, database *sql.DB, prefix string, number int64) [][]tgbotapi.InlineKeyboardButton {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		return [][]tgbotapi.InlineKeyboardButton{
			fsmutil.BackCancelRow("export_back", "export_cancel"),
		}
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range classes {
		if int64(c.Number) != number {
			continue
		}
		cb := fmt.Sprintf("%s%s", prefix, strings.ToUpper(c.Letter))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(strings.ToUpper(c.Letter), cb),
		))
	}
	rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
	return rows
}

// StartExportFSM стартовое меню (новое сообщение)
func StartExportFSM(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	u, _ := db.GetUserByTelegramID(ctx, database, chatID)
	if u == nil || !fsmutil.MustBeActiveForOps(u) {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Доступ временно закрыт. Обратитесь к администратору.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
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
	if _, err := tg.Send(bot, msgOut); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func HandleExportCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
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
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
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
			rows := exportClassNumberRowsFromDB(ctx, database, "export_class_number_")
			editMenu(bot, chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", rows)
			return
		case ExportStepStudentSelect:
			state.Step = ExportStepClassLetter
			rows := exportClassLetterRowsFromDB(ctx, database, "export_class_letter_", state.ClassNumber)
			editMenu(bot, chatID, cq.Message.MessageID, "🔠 Выберите букву класса:", rows)
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
			if _, err := tg.Send(bot, cfg); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		case ExportStepSchoolYearSelect:
			state.Step = ExportStepPeriodMode
			editMenu(bot, chatID, cq.Message.MessageID, "📅 Выберите режим периода:", periodModeRows())
			return

		default:
			delete(exportStates, chatID)
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Экспорт отменён.")
			if _, err := tg.Send(bot, edit); err != nil {
				metrics.HandlerErrors.Inc()
			}
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
		switch data {
		case "export_mode_fixed":
			state.PeriodMode = "fixed"
			state.Step = ExportStepFixedPeriodSelect
			_ = db.SetActivePeriod(ctx, database)
			periods, err := db.ListPeriods(ctx, database)
			if err != nil || len(periods) == 0 {
				delete(exportStates, chatID)
				edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ Не удалось загрузить периоды.")
				if _, err := tg.Send(bot, edit); err != nil {
					metrics.HandlerErrors.Inc()
				}
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

		case "export_mode_custom":
			state.PeriodMode = "custom"
			state.Step = ExportStepCustomStartDate

			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)

			rows := [][]tgbotapi.InlineKeyboardButton{
				fsmutil.BackCancelRow("export_back", "export_cancel"),
			}
			msg := tgbotapi.NewMessage(chatID, "📆 Введите дату начала (ДД.ММ.ГГГГ):")
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
			// для текстовых шагов неизбежно создаём новое сообщение
			if _, err := tg.Send(bot, msg); err != nil {
				metrics.HandlerErrors.Inc()
			}
		case "export_mode_schoolyear":
			state.PeriodMode = "schoolyear"
			state.Step = ExportStepSchoolYearSelect
			editMenu(bot, chatID, cq.Message.MessageID, "📘 Выберите учебный год:", schoolYearRows("export_schoolyear_"))
			return
		}

	case ExportStepFixedPeriodSelect:
		if strings.HasPrefix(data, "export_period_") {
			idStr := strings.TrimPrefix(data, "export_period_")
			id, _ := strconv.ParseInt(idStr, 10, 64)
			state.PeriodID = &id

			// дальше — в зависимости от типа отчёта
			if state.ReportType == "school" {
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cq.ID, "📥 Отчёт формируется...")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				generateExportReport(ctx, bot, database, chatID, state)
				delete(exportStates, chatID)
				return
			}
			// student / class → выбор номера класса (редактирование)
			state.Step = ExportStepClassNumber
			rows := exportClassNumberRowsFromDB(ctx, database, "export_class_number_")
			editMenu(bot, chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", rows)
		}

	case ExportStepClassNumber:
		if strings.HasPrefix(data, "export_class_number_") {
			state.ClassNumber, _ = strconv.ParseInt(strings.TrimPrefix(data, "export_class_number_"), 10, 64)
			state.Step = ExportStepClassLetter
			rows := exportClassLetterRowsFromDB(ctx, database, "export_class_letter_", state.ClassNumber)
			editMenu(bot, chatID, cq.Message.MessageID, "🔠 Выберите букву класса:", rows)
		}

	case ExportStepClassLetter:
		if strings.HasPrefix(data, "export_class_letter_") {
			state.ClassLetter = strings.TrimPrefix(data, "export_class_letter_")
			if state.ReportType == "student" {
				state.Step = ExportStepStudentSelect
				// тут нам важно оставить тот же message_id, поэтому редактируем только клавиатуру
				promptStudentSelectExport(ctx, bot, database, cq)
			} else if state.ReportType == "class" {
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cq.ID, "📥 Отчёт формируется...")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				generateExportReport(ctx, bot, database, chatID, state)
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
			promptStudentSelectExport(ctx, bot, database, cq)
			return
		} else if data == "export_students_done" {
			if len(state.SelectedStudentIDs) == 0 {
				if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Выберите хотя бы одного ученика.")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cq.ID, "📥 Отчёт формируется...")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			generateExportReport(ctx, bot, database, chatID, state)
			delete(exportStates, chatID)
		}
	case ExportStepSchoolYearSelect:
		if strings.HasPrefix(data, "export_schoolyear_") {
			startYear, _ := strconv.Atoi(strings.TrimPrefix(data, "export_schoolyear_"))
			from, to := db.SchoolYearBoundsByStartYear(startYear)
			state.PeriodMode = "schoolyear"
			state.FromDate, state.ToDate = &from, &to

			// Дальше поведение как в «произвольном» диапазоне:
			switch state.ReportType {
			case "student":
				state.SelectedStudentIDs = nil
				state.Step = ExportStepClassNumber
				rows := exportClassNumberRowsFromDB(ctx, database, "export_class_number_")
				editMenu(bot, chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", rows)
				return
			case "class":
				state.Step = ExportStepClassNumber
				rows := exportClassNumberRowsFromDB(ctx, database, "export_class_number_")
				editMenu(bot, chatID, cq.Message.MessageID, "🔢 Выберите номер класса:", rows)
				return
			case "school":
				// формируем отчёт немедленно
				if _, err := tg.Request(bot, tgbotapi.NewCallback(cq.ID, "📥 Отчёт формируется...")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				generateExportReport(ctx, bot, database, chatID, state)
				delete(exportStates, chatID)
				return
			}
		}
	}
}

func HandleExportText(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := exportStates[chatID]
	if state == nil {
		return
	}

	// текстовая отмена
	if fsmutil.IsCancelText(msg.Text) {
		delete(exportStates, chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Экспорт отменён.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
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
			if _, err := tg.Send(bot, msg); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		state.FromDate = &date
		state.Step = ExportStepCustomEndDate
		rows := [][]tgbotapi.InlineKeyboardButton{
			fsmutil.BackCancelRow("export_back", "export_cancel"),
		}
		msg := tgbotapi.NewMessage(chatID, "📅 Введите дату окончания (ДД.ММ.ГГГГ):")
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		if _, err := tg.Send(bot, msg); err != nil {
			metrics.HandlerErrors.Inc()
		}

	case ExportStepCustomEndDate:
		date, err := time.Parse("02.01.2006", strings.TrimSpace(msg.Text))
		if err != nil {
			rows := [][]tgbotapi.InlineKeyboardButton{
				fsmutil.BackCancelRow("export_back", "export_cancel"),
			}
			msg := tgbotapi.NewMessage(chatID, "❌ Неверный формат. Введите дату в формате ДД.ММ.ГГГГ.")
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
			if _, err := tg.Send(bot, msg); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		endOfDay := date.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		state.ToDate = &endOfDay

		// дальше как после выбора периода
		if state.ReportType == "school" {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "📥 Отчёт формируется...")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			generateExportReport(ctx, bot, database, chatID, state)
			delete(exportStates, chatID)
			return
		}
		state.Step = ExportStepClassNumber
		msgOut := tgbotapi.NewMessage(chatID, "🔢 Выберите номер класса:")
		rows := exportClassNumberRowsFromDB(ctx, database, "export_class_number_")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		if _, err := tg.Send(bot, msgOut); err != nil {
			metrics.HandlerErrors.Inc()
		}
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
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📘 Учебный год", "export_mode_schoolyear"),
		),
		fsmutil.BackCancelRow("export_back", "export_cancel"),
	}
}

// Единый редактор текста + клавиатуры
func editMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	if _, err := tg.Send(bot, cfg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

// Выбор студентов — редактируем только клавиатуру у текущего сообщения
func promptStudentSelectExport(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	state := exportStates[chatID]
	students, err := db.GetStudentsByClass(ctx, database, state.ClassNumber, state.ClassLetter)
	if err != nil {
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ Не удалось получить список учеников.")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

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
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func generateExportReport(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, state *ExportFSMState) {
	// защита от двойного запуска
	key := fmt.Sprintf("export:%d:%s", chatID, state.ReportType)
	if !fsmutil.SetPending(chatID, key) {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	defer fsmutil.ClearPending(chatID, key)

	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Формирую Excel-файл...")); err != nil {
		metrics.HandlerErrors.Inc()
	}
	taskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	go func(c context.Context) {
		defer cancel()
		var scores []models.ScoreWithUser
		var err error
		var periodLabel string

		switch state.PeriodMode {
		case "fixed":
			if state.PeriodID == nil {
				if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Период не выбран")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}
			p, errP := db.GetPeriodByID(c, database, int(*state.PeriodID))
			if errP != nil || p == nil {
				if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Период не найден.")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}
			periodLabel = p.Name
			switch state.ReportType {
			case "student":
				for _, id := range state.SelectedStudentIDs {
					part, err := db.GetScoresByStudentAndPeriod(c, database, id, int(*state.PeriodID))
					if err != nil {
						log.Println("Ошибка при получении баллов:", err)
					}
					scores = append(scores, part...)
				}
			case "class":
				scores, err = db.GetScoresByClassAndPeriod(c, database, state.ClassNumber, state.ClassLetter, *state.PeriodID)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			case "school":
				scores, err = db.GetScoresByPeriod(c, database, int(*state.PeriodID))
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			}

		case "custom":
			if state.FromDate == nil || state.ToDate == nil {
				if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Даты не заданы")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}
			periodLabel = fmt.Sprintf("%s–%s", state.FromDate.Format("02.01.2006"), state.ToDate.Format("02.01.2006"))
			switch state.ReportType {
			case "student":
				for _, id := range state.SelectedStudentIDs {
					part, err := db.GetScoresByStudentAndDateRange(c, database, id, *state.FromDate, *state.ToDate)
					if err != nil {
						log.Println("Ошибка при получении баллов:", err)
					}
					scores = append(scores, part...)
				}
			case "class":
				scores, err = db.GetScoresByClassAndDateRange(c, database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			case "school":
				scores, err = db.GetScoresByDateRange(c, database, *state.FromDate, *state.ToDate)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			}
		case "schoolyear":
			if state.FromDate == nil || state.ToDate == nil {
				if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Даты учебного года не заданы")); err != nil {
					metrics.HandlerErrors.Inc()
				}
				return
			}
			// Красивый ярлык периода: "2024–2025"
			periodLabel = db.SchoolYearLabel(db.CurrentSchoolYearStartYear(*state.FromDate))

			switch state.ReportType {
			case "student":
				for _, id := range state.SelectedStudentIDs {
					part, err := db.GetScoresByStudentAndDateRange(c, database, id, *state.FromDate, *state.ToDate)
					if err != nil {
						log.Println("Ошибка при получении баллов:", err)
					}
					scores = append(scores, part...)
				}
			case "class":
				scores, err = db.GetScoresByClassAndDateRange(c, database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			case "school":
				scores, err = db.GetScoresByDateRange(c, database, *state.FromDate, *state.ToDate)
				if err != nil {
					log.Println("Ошибка при получении баллов:", err)
				}
			}
		}

		if len(scores) == 0 {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🔎 Данных за выбранный период не найдено.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		var filePath string
		var collective int64
		var className string

		// --- Вычисляем коллективный рейтинг ---
		// Для отчёта по классу
		if state.ReportType == "class" && len(scores) > 0 {
			collective, className = report(c, state, database)
		}
		// Для отчёта по ученику — класс берём из выбранного состояния (учеников выбираем внутри класса)
		if state.ReportType == "student" && len(scores) > 0 {
			collective, className = report(c, state, database)
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
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Ошибка генерации отчёта.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}

		doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
		doc.Caption = fmt.Sprintf("📊 Отчёт за период: %s", periodLabel)
		if _, err := tg.Send(bot, doc); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}(taskCtx)
}

func GetExportState(userID int64) *ExportFSMState {
	return exportStates[userID]
}

func ClearExportState(userID int64) {
	delete(exportStates, userID)
}

func schoolYearRows(prefix string) [][]tgbotapi.InlineKeyboardButton {
	now := time.Now()
	cur := db.CurrentSchoolYearStartYear(now)
	var rows [][]tgbotapi.InlineKeyboardButton
	for y := cur; y >= cur-5; y-- {
		label := db.SchoolYearLabel(y)
		cb := fmt.Sprintf("%s%d", prefix, y)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, cb),
		))
	}
	rows = append(rows, fsmutil.BackCancelRow("export_back", "export_cancel"))
	return rows
}

func report(ctx context.Context, state *ExportFSMState, database *sql.DB) (collective int64, className string) {
	className = fmt.Sprintf("%d%s", int(state.ClassNumber), state.ClassLetter)
	auctionID := db.GetCategoryIDByName(ctx, database, "Аукцион")
	if state.PeriodID != nil {
		if classScores, err2 := db.GetScoresByClassAndPeriod(ctx, database, state.ClassNumber, state.ClassLetter, *state.PeriodID); err2 == nil {
			stu := map[int64]int{}
			for _, sc := range classScores {
				if sc.CategoryID == int64(auctionID) {
					continue
				}
				stu[sc.StudentID] += sc.Points
			}
			for _, tot := range stu {
				collective += int64((tot * 30) / 100)
			}
		}
	} else if state.FromDate != nil && state.ToDate != nil {
		if classScores, err2 := db.GetScoresByClassAndDateRange(ctx, database, int(state.ClassNumber), state.ClassLetter, *state.FromDate, *state.ToDate); err2 == nil {
			stu := map[int64]int{}
			for _, sc := range classScores {
				if sc.CategoryID == int64(auctionID) {
					continue
				}
				stu[sc.StudentID] += sc.Points
			}
			for _, tot := range stu {
				collective += int64((tot * 30) / 100)
			}
		}
	}
	return collective, className
}
