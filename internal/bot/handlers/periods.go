package handlers

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"log"
	"strconv"
)

func ShowPeriods(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, isAdmin bool) {
	periods, err := db.ListPeriods(database)
	if err != nil || len(periods) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось получить список периодов."))
		return
	}

	for _, p := range periods {
		text := fmt.Sprintf("📘 Период: %s\n📅 %s → %s", p.Name,
			p.StartDate.Format("02.01.2006"), p.EndDate.Format("02.01.2006"))
		if p.IsActive {
			text += " ✅ (активный)"
		}

		msg := tgbotapi.NewMessage(chatID, text)

		if isAdmin && !p.IsActive {
			btn := tgbotapi.NewInlineKeyboardButtonData("Сделать активным", fmt.Sprintf("activate_period_%d", p.ID))
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btn))
		}
		bot.Send(msg)
	}
}

func HandlePeriodCallback(cb *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, database *sql.DB) {
	data := cb.Data
	if !cb.From.IsBot && data != "" && data[:15] == "activate_period" {
		idStr := data[len("activate_period_"):]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			log.Println("⚠️ Неверный ID периода:", err)
			return
		}
		err = db.SetActivePeriod(database, id)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Не удалось активировать период."))
			return
		}
		bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "✅ Период активирован."))
	}
}
