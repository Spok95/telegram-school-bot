package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
)

func SendConsultReminder(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, slot db.ConsultSlot, due string, loc *time.Location) error {
	if !slot.BookedByID.Valid {
		// слот не забронирован — слать нечего
		return nil
	}

	// кого оповещаем
	parent, err := db.GetUserByID(ctx, database, slot.BookedByID.Int64)
	if err != nil {
		return err
	}
	teacher, err := db.GetUserByID(ctx, database, slot.TeacherID)
	if err != nil {
		return err
	}

	parentChat := parent.TelegramID
	teacherChat := teacher.TelegramID
	if parentChat == 0 || teacherChat == 0 {
		// у кого-то нет telegram_id — просто вывалимся (можно залогировать в Sentry с деталями, если захочешь)
		return nil
	}

	whenStart := slot.StartAt.In(loc)
	whenEnd := slot.EndAt.In(loc)

	var prefix string
	switch due {
	case "24 hours":
		prefix = "Напоминание за 24 часа"
	default:
		prefix = "Напоминание за 1 час"
	}

	timeWindow := fmt.Sprintf("%s–%s", whenStart.Format("02.01.2006 15:04"), whenEnd.Format("15:04"))

	// Тексты: можно обогатить именами/классами — сейчас минимально, чтобы не зависеть от лишних join'ов
	textParent := fmt.Sprintf("%s: консультация у учителя %s.", prefix, timeWindow)
	textTeacher := fmt.Sprintf("%s: консультация с родителем %s.", prefix, timeWindow)

	if _, err := bot.Send(tgbotapi.NewMessage(parentChat, textParent)); err != nil {
		return err
	}
	if _, err := bot.Send(tgbotapi.NewMessage(teacherChat, textTeacher)); err != nil {
		return err
	}
	return nil
}

// SendConsultBookedNotification — моментальная нотификация о записи (оба адресата)
func SendConsultBookedNotification(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, slot db.ConsultSlot, loc *time.Location) error {
	if !slot.BookedByID.Valid {
		return nil
	}
	parent, err := db.GetUserByID(ctx, database, slot.BookedByID.Int64)
	if err != nil {
		return err
	}
	teacher, err := db.GetUserByID(ctx, database, slot.TeacherID)
	if err != nil {
		return err
	}
	parentChat := parent.TelegramID
	teacherChat := teacher.TelegramID
	if parentChat == 0 || teacherChat == 0 {
		// у кого-то не привязан Telegram — тихо выходим
		return nil
	}

	whenStart := slot.StartAt.In(loc)
	whenEnd := slot.EndAt.In(loc)
	win := fmt.Sprintf("%s–%s", whenStart.Format("02.01.2006 15:04"), whenEnd.Format("15:04"))

	textParent := fmt.Sprintf("Запись подтверждена: консультация у учителя %s.", win)
	textTeacher := fmt.Sprintf("Новая запись: консультация с родителем %s.", win)

	if _, err := bot.Send(tgbotapi.NewMessage(parentChat, textParent)); err != nil {
		return err
	}
	if _, err := bot.Send(tgbotapi.NewMessage(teacherChat, textTeacher)); err != nil {
		return err
	}
	return nil
}

// SendTeacherCancelNotification — учитель отменил запись: уведомляем родителя
func SendTeacherCancelNotification(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, parentUserID int64, slot db.ConsultSlot, loc *time.Location) error {
	parent, err := db.GetUserByID(ctx, database, parentUserID)
	if err != nil {
		return err
	}
	if parent.TelegramID == 0 {
		return nil
	}

	win := fmt.Sprintf("%s–%s",
		slot.StartAt.In(loc).Format("02.01.2006 15:04"),
		slot.EndAt.In(loc).Format("15:04"),
	)
	text := fmt.Sprintf("Запись на консультацию %s была отменена учителем.", win)
	_, err = bot.Send(tgbotapi.NewMessage(parent.TelegramID, text))
	return err
}
