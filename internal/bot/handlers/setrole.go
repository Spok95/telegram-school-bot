package handlers

import (
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"gopkg.in/telebot.v3"
)

var (
	SetRoleKeyboard = &telebot.ReplyMarkup{}
	btnStudent      = SetRoleKeyboard.Data("Ученик", "setrole_student", "student")
	btnTeacher      = SetRoleKeyboard.Data("Учитель", "setrole_teacher", "teacher")
	btnParent       = SetRoleKeyboard.Data("Родитель", "setrole_parent", "parent")
	btnAdmin        = SetRoleKeyboard.Data("Админ", "setrole_admin", "admin")
)

func SetRoleHandler(c telebot.Context) error {
	SetRoleKeyboard.Inline(
		SetRoleKeyboard.Row(btnStudent, btnTeacher),
		SetRoleKeyboard.Row(btnParent, btnAdmin),
	)
	return c.Send("Выберите вашу роль:", SetRoleKeyboard)
}

func InitSetRole(bot *telebot.Bot) {
	handle := func(role models.Role) func(c telebot.Context) error {
		return func(c telebot.Context) error {
			user := c.Sender()
			err := db.SetUserRole(user.ID, user.FirstName+" "+user.LastName, role)
			if err != nil {
				return c.Send("Ошибка при сохранении роли.")
			}
			return c.Send(fmt.Sprintf("Ваша роль установлена: %s", role))
		}
	}
	bot.Handle(&btnStudent, handle(models.Student))
	bot.Handle(&btnTeacher, handle(models.Teacher))
	bot.Handle(&btnParent, handle(models.Parent))
	bot.Handle(&btnAdmin, handle(models.Admin))
}
