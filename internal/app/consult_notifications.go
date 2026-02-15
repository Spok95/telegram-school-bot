package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/models"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
)

func SendConsultReminder(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, slot db.ConsultSlot, due string) error {
	if !slot.BookedByID.Valid {
		return nil
	}

	// участники
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
		return nil
	}

	// дочитываем, что именно было забронировано (класс/ребёнок)
	var bookedClassID sql.NullInt64
	var bookedChildID sql.NullInt64
	_ = database.QueryRowContext(ctx, `
		SELECT booked_class_id, booked_child_id
		FROM consult_slots
		WHERE id = $1
	`, slot.ID).Scan(&bookedClassID, &bookedChildID)

	// класс
	className := ""
	if bookedClassID.Valid {
		if cls, _ := db.GetClassByID(ctx, database, bookedClassID.Int64); cls != nil {
			className = fmt.Sprintf("%d%s", cls.Number, strings.ToUpper(cls.Letter))
		}
	}

	// ребёнок
	childName := ""
	if bookedChildID.Valid {
		if ch, err := db.GetUserByID(ctx, database, bookedChildID.Int64); err == nil {
			childName = ch.Name
		}
	}

	// префикс
	var prefixParent string
	var prefixTeacher string
	switch due {
	case "24 hours":
		prefixParent = "Напоминаем, что завтра"
		prefixTeacher = "Напоминаем, что завтра"
	default:
		prefixParent = "Напоминаем, что через час"
		prefixTeacher = "Напоминаем, что через час"
	}

	timeWindow := fmt.Sprintf("%s–%s", slot.StartAt.Format("02.01.2006 15:04"), slot.EndAt.Format("15:04"))

	// формат + ссылка
	fmtLabel := "оффлайн"
	if slot.ConsultFormat == "online" {
		fmtLabel = "онлайн"
	}
	link := ""
	if slot.ConsultFormat == "online" && slot.OnlineURL.Valid && strings.TrimSpace(slot.OnlineURL.String) != "" {
		link = strings.TrimSpace(slot.OnlineURL.String)
	}

	// Родителю (строго по ТЗ)
	textParent := fmt.Sprintf(
		`%s Вы записаны на консультацию %s к %s, формат: %s`,
		prefixParent, timeWindow, teacher.Name, fmtLabel,
	)
	if link != "" {
		textParent += "\nСсылка: " + link
	}

	// Учителю (строго по ТЗ, без “фио учителя” внутри — учитель и так адресат)
	// (если className/childName пустые — просто будут пустые поля, это нормально)
	textTeacher := fmt.Sprintf(
		`%s к Вам записаны на консультацию %s, родитель: %s, ребёнок: %s, класс: %s, формат: %s`,
		prefixTeacher, timeWindow, parent.Name, childName, className, fmtLabel,
	)
	if link != "" {
		textTeacher += "\nСсылка: " + link
	}

	if _, err := tg.Send(bot, tgbotapi.NewMessage(parentChat, textParent)); err != nil {
		metrics.HandlerErrors.Inc()
	}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(teacherChat, textTeacher)); err != nil {
		metrics.HandlerErrors.Inc()
	}

	return nil
}

// SendConsultBookedNotification — моментальная нотификация о записи (оба адресата)
func SendConsultBookedNotification(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, slot db.ConsultSlot) error {
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

	whenStart := slot.StartAt
	whenEnd := slot.EndAt
	win := fmt.Sprintf("%s–%s", whenStart.Format("02.01.2006 15:04"), whenEnd.Format("15:04"))

	textParent := fmt.Sprintf("Запись подтверждена: консультация у учителя %s.", win)
	textTeacher := fmt.Sprintf("Новая запись: консультация с родителем %s.", win)

	if _, err := tg.Send(bot, tgbotapi.NewMessage(parentChat, textParent)); err != nil {
		metrics.HandlerErrors.Inc()
	}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(teacherChat, textTeacher)); err != nil {
		metrics.HandlerErrors.Inc()
	}
	return nil
}

// SendConsultBookedCard карточки при брони
func SendConsultBookedCard(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, slot db.ConsultSlot, parent models.User, child models.User) error {
	teacher, err := db.GetUserByID(ctx, database, slot.TeacherID)
	if err != nil {
		return err
	}

	var classID int64
	if child.ClassID != nil {
		classID = *child.ClassID
	} else {
		classID = slot.ClassID
	}
	class, _ := db.GetClassByID(ctx, database, classID)

	when := fmt.Sprintf("%s — %s",
		slot.StartAt.Format("02.01.2006 15:04"),
		slot.EndAt.Format("15:04"),
	)
	className := ""
	if class != nil {
		className = fmt.Sprintf("%d%s", class.Number, strings.ToUpper(class.Letter))
	}

	fmtLabel := "оффлайн"
	if slot.ConsultFormat == "online" {
		fmtLabel = "онлайн"
	}
	link := ""
	if slot.ConsultFormat == "online" && slot.OnlineURL.Valid && strings.TrimSpace(slot.OnlineURL.String) != "" {
		link = strings.TrimSpace(slot.OnlineURL.String)
	}

	// родителю
	textParent := fmt.Sprintf(
		"📌 Вы записаны на консультацию\nДата/время: %s\nУчитель: %s\nКласс: %s\nФормат: %s",
		when, teacher.Name, className, fmtLabel,
	)
	if link != "" {
		textParent += "\nСсылка: " + link
	}
	if parent.TelegramID != 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(parent.TelegramID, textParent)); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}

	// учителю
	textTeacher := fmt.Sprintf(
		"📌 Новая запись на консультацию\nДата/время: %s\nРодитель: %s\nРебёнок: %s\nКласс: %s\nФормат: %s",
		when, parent.Name, child.Name, className, fmtLabel,
	)
	if link != "" {
		textTeacher += "\nСсылка: " + link
	}
	if teacher.TelegramID != 0 {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(teacher.TelegramID, textTeacher)); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}

	return nil
}

// SendConsultCancelCards — карточки об отмене: учителю и родителю + бродкаст по классу.
func SendConsultCancelCards(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, parentID int64, slot db.ConsultSlot) error {
	// участники
	parent, err := db.GetUserByID(ctx, database, parentID)
	if err != nil {
		return err
	}
	teacher, err := db.GetUserByID(ctx, database, slot.TeacherID)
	if err != nil {
		return err
	}

	// дочитываем, что именно было забронировано
	var bookedClassID sql.NullInt64
	var bookedChildID sql.NullInt64
	_ = database.QueryRowContext(ctx, `
	    SELECT booked_class_id, booked_child_id
	    FROM consult_slots
	    WHERE id = $1
	`, slot.ID).Scan(&bookedClassID, &bookedChildID)

	// определяем класс для сообщения
	classID := slot.ClassID
	if bookedClassID.Valid {
		classID = bookedClassID.Int64
	}
	class, err := db.GetClassByID(ctx, database, classID)
	if err != nil {
		return err
	}

	// определяем имя ребёнка
	var childName string
	if bookedChildID.Valid {
		ch, err := db.GetUserByID(ctx, database, bookedChildID.Int64)
		if err == nil {
			childName = ch.Name
		}
	} else {
		// старый fallback: поиск ребёнка этого родителя в этом классе
		if ch, err := db.GetChildByParentAndClass(ctx, database, parentID, classID); err == nil && ch != nil {
			childName = ch.Name
		}
	}

	classLabel := fmt.Sprintf("%d%s", class.Number, class.Letter)
	start := slot.StartAt.Format("02.01.2006 15:04")
	end := slot.EndAt.Format("15:04")

	// --- учителю
	if teacher.TelegramID != 0 {
		teacherText := fmt.Sprintf(
			"Отменена запись на\n%s — %s\nродитель: %s\nученик: %s\nкласс: %s",
			start, end, parent.Name, childName, classLabel,
		)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(teacher.TelegramID, teacherText)); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}

	// --- родителю
	if parent.TelegramID != 0 {
		parentText := fmt.Sprintf(
			"⚠️ Ваша запись на консультацию\nДата/время: %s — %s\nУчитель: %s\nКласс: %s\nОтменена",
			start, end, teacher.Name, classLabel,
		)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(parent.TelegramID, parentText)); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
	return nil
}
