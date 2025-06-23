package main

import (
	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/joho/godotenv"
	"gopkg.in/telebot.v3"
	"log"
	"os"
	"time"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	db.InitDB()
	db.Migrate()

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN environment variable not set")
	}

	bot, err := telebot.NewBot(telebot.Settings{
		Token:  token,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	bot.Handle("/start", handlers.StartHandler)
	bot.Handle(&handlers.BtnOpenMenu, handlers.MenuHandler)
	bot.Handle("/menu", handlers.MenuHandler)
	bot.Handle("/setrole", handlers.SetRoleHandler)
	bot.Handle("/my_score", handlers.MyScoreHandler)
	handlers.InitMenu(bot)
	handlers.InitSetRole(bot)
	handlers.InitAwardHandler(bot)

	log.Println("Bot is running")
	bot.Start()
}
