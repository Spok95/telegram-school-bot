package handlers

import (
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"gopkg.in/telebot.v3"
)

func MyScoreHandler(c telebot.Context) error {
	user := c.Sender()

	fmt.Printf("[MYSCORE] Telegram ID: %d\n", user.ID)

	u, err := db.GetUserByTelegramID(user.ID)
	if err != nil {
		return c.Send("Пользователь не найден. Пожалуйста, установите роль командой /setrole.")
	}

	if u.Role != "student" {
		return c.Send(fmt.Sprintf("Вы зарегистрированы как %s. Только ученики могут просматривать рейтинг.", u.Role))
	}

	return c.Send("🧮 Ваш текущий рейтинг: 0 баллов\n(баллы появятся позже)")
}
