package handlers

import (
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"gopkg.in/telebot.v3"
	"strconv"
)

func InitAwardHandler(bot *telebot.Bot) {
	bot.Handle(&BtnAward, func(c telebot.Context) error {
		// Получаем всех учеников
		students, err := db.GetAllStudents()
		if err != nil {
			return c.Send("Ошибка при получении учеников.")
		}
		if len(students) == 0 {
			return c.Send("Нет зарегистрированных учеников.")
		}

		// Генерируем клавиатуру со списком учеников
		markup := &telebot.ReplyMarkup{}
		var buttons []telebot.Btn
		for _, s := range students {
			btn := markup.Data(s.Name, fmt.Sprintf("award_student_%d", s.ID), strconv.FormatInt(s.ID, 10))
			buttons = append(buttons, btn)
		}
		// По 2 в ряд
		markup.Inline(markup.Split(2, buttons)...)

		return c.Send("Выберите ученика для начисления баллов:", markup)
	})
}
