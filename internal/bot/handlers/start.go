package handlers

import "gopkg.in/telebot.v3"

func StartHandler(c telebot.Context) error {
	return c.Send("Добро пожаловать в школьный рейтинг! 🎓")
}
