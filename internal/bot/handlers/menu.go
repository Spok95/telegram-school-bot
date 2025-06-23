package handlers

import "gopkg.in/telebot.v3"

var (
	menuKeyboard = &telebot.ReplyMarkup{}

	btnMyScore  = menuKeyboard.Text("🧮 Показать рейтинг")
	BtnAward    = menuKeyboard.Text("➕ Начислить баллы")
	btnDeduct   = menuKeyboard.Text("➖ Списать баллы")
	btnSetRole  = menuKeyboard.Text("🔐 Установить роль")
	BtnOpenMenu = menuKeyboard.Data("📋 Открыть меню", "open_menu")
)

func MenuHandler(c telebot.Context) error {
	menuKeyboard.Reply(
		menuKeyboard.Row(btnMyScore),
		menuKeyboard.Row(BtnAward, btnDeduct),
		menuKeyboard.Row(btnSetRole),
	)
	return c.Send("📋 Главное меню", menuKeyboard)
}

func InitMenu(bot *telebot.Bot) {
	bot.Handle(&btnMyScore, func(c telebot.Context) error {
		return c.Send("/my_score")
	})

	bot.Handle(&BtnAward, func(c telebot.Context) error {
		return c.Send("/award")
	})

	bot.Handle(&btnDeduct, func(c telebot.Context) error {
		return c.Send("/deduct")
	})

	bot.Handle(&btnSetRole, func(c telebot.Context) error {
		return SetRoleHandler(c)
	})
}
