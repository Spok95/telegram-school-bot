package menu

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

// GetRoleMenu возвращает меню в зависимости от роли пользователя
func GetRoleMenu(role string) tgbotapi.ReplyKeyboardMarkup {
	switch role {
	case "student":
		return studentMenu()
	case "teacher":
		return teacherMenu()
	case "parent":
		return parentMenu()
	case "admin", "administration":
		return adminMenu()
	default:
		return tgbotapi.NewReplyKeyboard() // пустое меню
	}
}

func studentMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Мой рейтинг"),
		),
	)
}

func teacherMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Начислить баллы"),
			tgbotapi.NewKeyboardButton("📉 Списать баллы"),
		),
	)
}

func adminMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Начислить баллы"),
			tgbotapi.NewKeyboardButton("📉 Списать баллы"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Заявки на баллы"),
			tgbotapi.NewKeyboardButton("📥 Заявки на авторизацию"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Отчёты"),
			tgbotapi.NewKeyboardButton("📥 Экспорт отчёта"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📅 Установить период"),
		),
	)
}

func parentMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Рейтинг ребёнка"),
		),
	)
}
