package handlers

import "gopkg.in/telebot.v3"

func StartHandler(c telebot.Context) error {
	menuBtn := &telebot.ReplyMarkup{}
	menuBtn.Inline(menuBtn.Row(BtnOpenMenu))

	return c.Send("Добро пожаловать в школьный рейтинг! 🎓", menuBtn)
}
