package handlers

import (
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"gopkg.in/telebot.v3"
)

func SetRoleHandler(c telebot.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Send("Укажите роль: /setrole student|teacher|parent|admin")
	}

	roleStr := args[0]
	var role models.Role
	switch roleStr {
	case "student", "teacher", "parent", "admin":
		role = models.Role(roleStr)
	default:
		return c.Send("Неверная роль. Возможные: student, teacher, parent, admin")
	}

	user := c.Sender()
	err := db.SetUserRole(user.ID, user.FirstName+" "+user.LastName, role)
	if err != nil {
		return c.Send("Ошибка при сохранении роли.")
	}
	return c.Send("Ваша роль установлена: " + roleStr)
}
