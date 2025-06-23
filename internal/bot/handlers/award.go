package handlers

import (
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"gopkg.in/telebot.v3"
	"strconv"
	"strings"
)

var awardTemp = make(map[int64]struct {
	StudentID int64
	Category  string
})

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
	bot.Handle(&telebot.Callback{Unique: "award_student"}, func(c telebot.Context) error {
		// Извлекаем student_id из callback data
		parts := strings.Split(c.Callback().Data, "_")
		if len(parts) < 3 {
			return c.Send("Неверный формат данных.")
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return c.Send("Ошибка чтения ID ученика.")
		}

		// Сохраняем ID ученика во временное хранилище (например, в память или context)
		userID := c.Sender().ID
		awardTemp[userID] = struct {
			StudentID int64
			Category  string
		}{
			StudentID: id,
		}

		// Отправляем форму выбора категории
		markup := &telebot.ReplyMarkup{}

		btnHonor := markup.Data("Честь", "award_category", "honor")
		btnStudy := markup.Data("Учёба", "award_category", "study")
		btnDiscipline := markup.Data("Дисциплина", "award_category", "discipline")

		markup.Inline(
			markup.Row(btnHonor, btnStudy),
			markup.Row(btnDiscipline),
		)
		return c.Send("Выберите категорию для начисления баллов:", markup)
	})
	bot.Handle(&telebot.Callback{Unique: "award_category"}, func(c telebot.Context) error {
		userID := c.Sender().ID
		category := c.Data()

		// Загружаем предыдущее состояние
		temp := awardTemp[userID]
		temp.Category = category
		awardTemp[userID] = temp

		return c.Send("Введите количество баллов (например: 100 или 200):")
	})
}
