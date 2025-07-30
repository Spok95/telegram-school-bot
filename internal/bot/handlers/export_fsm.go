package handlers

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strconv"
)

type ExportFSMState struct {
	Step       int
	ReportType string
	PeriodID   int64
}

var exportStates = make(map[int64]*ExportFSMState)

func StartExportFSM(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
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
		go generateExport(bot, database, chatID, *state)

		delete(exportStates, chatID)
	}
}

func generateExport(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, state ExportFSMState) {
	bot.Send(tgbotapi.NewMessage(chatID, "📂 (заглушка) Отчёт сформирован: тип "+state.ReportType+", период ID "+strconv.FormatInt(state.PeriodID, 10)))
}
