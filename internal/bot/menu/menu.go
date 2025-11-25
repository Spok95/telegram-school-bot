package menu

import (
	"os"

	"github.com/Spok95/telegram-school-bot/internal/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// читаем флажок один раз при старте
var consultationsEnabled = os.Getenv("CONSULTATIONS_ENABLED") != "false"

// GetRoleMenu возвращает меню в зависимости от роли пользователя
func GetRoleMenu(role string) tgbotapi.ReplyKeyboardMarkup {
	switch role {
	case string(models.Student):
		return studentMenu()
	case string(models.Teacher):
		return teacherMenu()
	case string(models.Parent):
		return parentMenu()
	case string(models.Admin):
		return adminMenu()
	case string(models.Administration):
		return administrationMenu()
	default:
		return tgbotapi.NewReplyKeyboard() // пустое меню
	}
}

func studentMenu() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Мой рейтинг"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📜 История получения баллов"),
		),
	)
}

func teacherMenu() tgbotapi.ReplyKeyboardMarkup {
	// базовые кнопки учителя
	rows := []tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButton("➕ Начислить баллы"),
		tgbotapi.NewKeyboardButton("📉 Списать баллы"),
	}

	kbRows := [][]tgbotapi.KeyboardButton{
		rows,
	}

	// консультации — только если включены
	if consultationsEnabled {
		kbRows = append(kbRows,
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("🗓 Создать слоты"),
				tgbotapi.NewKeyboardButton("📋 Мои слоты"),
				tgbotapi.NewKeyboardButton("📘 Расписание консультаций"),
			),
		)
	}

	return tgbotapi.NewReplyKeyboard(kbRows...)
}

func administrationMenu() tgbotapi.ReplyKeyboardMarkup {
	kbRows := [][]tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Начислить баллы"),
			tgbotapi.NewKeyboardButton("📉 Списать баллы"),
			tgbotapi.NewKeyboardButton("🎯 Аукцион"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Заявки на баллы"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Экспорт отчёта"),
		),
	}

	// отчёт по консультациям — только если включены
	if consultationsEnabled {
		kbRows = append(kbRows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📈 Отчёт консультаций")),
		)
	}

	return tgbotapi.NewReplyKeyboard(kbRows...)
}

func adminMenu() tgbotapi.ReplyKeyboardMarkup {
	kbRows := [][]tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Начислить баллы"),
			tgbotapi.NewKeyboardButton("📉 Списать баллы"),
			tgbotapi.NewKeyboardButton("🎯 Аукцион"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Заявки на баллы"),
			tgbotapi.NewKeyboardButton("📥 Заявки на авторизацию"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Экспорт отчёта"),
			tgbotapi.NewKeyboardButton("🗂 Справочники"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📅 Периоды"),
			tgbotapi.NewKeyboardButton("👥 Пользователи"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💾 Бэкап БД"),
			tgbotapi.NewKeyboardButton("♻️ Восстановить БД"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Восстановить из файла"),
		),
	}

	// добавим "📈 Отчёт консультаций", если включено
	if consultationsEnabled {
		// вставим в третью строку
		kbRows[2] = tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📥 Экспорт отчёта"),
			tgbotapi.NewKeyboardButton("🗂 Справочники"),
			tgbotapi.NewKeyboardButton("📈 Отчёт консультаций"),
		)
	}

	return tgbotapi.NewReplyKeyboard(kbRows...)
}

func parentMenu() tgbotapi.ReplyKeyboardMarkup {
	kbRows := [][]tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Рейтинг ребёнка"),
			tgbotapi.NewKeyboardButton("➕ Добавить ребёнка"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📜 История получения баллов"),
		),
	}

	// запись на консультацию — только если включены
	if consultationsEnabled {
		kbRows = append(kbRows,
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("📅 Записаться на консультацию"),
				tgbotapi.NewKeyboardButton("📋 Мои записи"),
			),
		)
	}

	return tgbotapi.NewReplyKeyboard(kbRows...)
}
