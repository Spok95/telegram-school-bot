package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type ParentFSMState string

const (
	StateParentName        ParentFSMState = "parent_name"
	StateParentStudentName ParentFSMState = "parent_student_name"
	StateParentClassNumber ParentFSMState = "parent_class_number"
	StateParentClassLetter ParentFSMState = "parent_class_letter"
	StateParentWaiting     ParentFSMState = "parent_waiting"
)

var (
	parentFSM  = make(map[int64]ParentFSMState)
	parentData = make(map[int64]*ParentRegisterData)
)

type ParentRegisterData struct {
	StudentName string
	ClassNumber int
	ClassLetter string
	ParentName  string
}

func parentBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("parent_back", "parent_cancel")
}

func parentClassNumberRows() [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		cb := fmt.Sprintf("parent_class_num_%d", i)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", i), cb),
		))
	}
	rows = append(rows, parentBackCancelRow())
	return rows
}

func parentClassLetterRows() [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l, "parent_class_letter_"+l),
		))
	}
	rows = append(rows, parentBackCancelRow())
	return rows
}

func parentEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	if _, err := tg.Send(bot, cfg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func StartParentRegistration(ctx context.Context, chatID int64, user *tgbotapi.User, bot *tgbotapi.BotAPI) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	parentFSM[chatID] = StateParentName
	parentData[chatID] = &ParentRegisterData{}
	if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Введите ваше ФИО:")); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func HandleParentFSM(ctx context.Context, chatID int64, msg string, bot *tgbotapi.BotAPI) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	trimmed := strings.TrimSpace(msg)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	state := parentFSM[chatID]

	if state == StateParentName {
		if parentData[chatID] == nil {
			parentData[chatID] = &ParentRegisterData{}
		}
		parentData[chatID].ParentName = db.ToTitleRU(strings.TrimSpace(msg))
		parentFSM[chatID] = StateParentStudentName
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "Введите ФИО ребёнка, которого вы представляете:")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	if state == StateParentStudentName {
		if parentData[chatID] == nil {
			parentData[chatID] = &ParentRegisterData{}
		}
		parentData[chatID].StudentName = msg
		parentData[chatID].StudentName = db.ToTitleRU(strings.TrimSpace(msg))
		parentFSM[chatID] = StateParentClassNumber
		msgOut := tgbotapi.NewMessage(chatID, "Выберите номер класса ребёнка:")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(parentClassNumberRows()...)
		if _, err := tg.Send(bot, msgOut); err != nil {
			metrics.HandlerErrors.Inc()
		}
	}
}

func HandleParentCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	data := cq.Data
	state := parentFSM[chatID]

	if data == "parent_cancel" {
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Регистрация отменена. Нажмите /start, чтобы начать заново.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}
	if data == "parent_back" {
		switch state {
		case StateParentStudentName:
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Введите ваше ФИО:")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			parentFSM[chatID] = StateParentName
		case StateParentClassNumber:
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Введите ФИО ребёнка:")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			parentFSM[chatID] = StateParentStudentName
		case StateParentClassLetter:
			parentFSM[chatID] = StateParentClassNumber
			parentEditMenu(bot, chatID, cq.Message.MessageID, "Выберите номер класса ребёнка:", parentClassNumberRows())
		case StateParentWaiting:
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cq.ID, "Заявка уже отправлена, ожидайте подтверждения.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		default:
			if _, err := tg.Request(bot, tgbotapi.NewCallback(cq.ID, "Действие недоступно на этом шаге.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
		}
		return
	}

	if strings.HasPrefix(data, "parent_class_num_") {
		numStr := strings.TrimPrefix(data, "parent_class_num_")
		num, _ := strconv.Atoi(numStr)
		if parentData[chatID] == nil {
			parentData[chatID] = &ParentRegisterData{}
		}
		parentData[chatID].ClassNumber = num
		parentFSM[chatID] = StateParentClassLetter
		parentEditMenu(bot, chatID, cq.Message.MessageID, "Выберите букву класса:", parentClassLetterRows())
		return
	}

	if strings.HasPrefix(data, "parent_class_letter_") {
		letter := strings.TrimPrefix(data, "parent_class_letter_")
		parentData[chatID].ClassLetter = letter
		parentFSM[chatID] = StateParentWaiting

		studentID, err := FindStudentID(ctx, database, parentData[chatID])
		if err != nil {
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "❌ Ученик не найден. Введите ФИО заново:")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			parentFSM[chatID] = StateParentStudentName
			return
		}

		parentID, err := SaveParentRequest(ctx, database, chatID, studentID, parentData[chatID].ParentName)
		if err != nil {
			fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
			if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Ошибка при сохранении. Попробуйте позже.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			delete(parentFSM, chatID)
			delete(parentData, chatID)
			return
		}
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		if _, err := tg.Send(bot, tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "Заявка на регистрацию родителя отправлена администратору. Ожидайте подтверждения.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		handlers.NotifyAdminsAboutNewUser(ctx, bot, database, parentID)
		delete(parentFSM, chatID)
		delete(parentData, chatID)
		return
	}
}

func FindStudentID(ctx context.Context, database *sql.DB, data *ParentRegisterData) (int, error) {
	var id int
	// Полное равенство без учета регистра: UPPER(name) = UPPER($1)
	// (работает корректно и для кириллицы в Postgres)
	err := database.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE UPPER(name) = UPPER($1)
		  AND class_number = $2
		  AND UPPER(class_letter) = UPPER($3)
		  AND role = 'student' AND confirmed = TRUE AND is_active = TRUE
	`, data.StudentName, data.ClassNumber, data.ClassLetter).Scan(&id)
	return id, err
}

func SaveParentRequest(ctx context.Context, database *sql.DB, parentTelegramID int64, studentID int, parentName string) (int64, error) {
	var parentID int64
	tx, err := database.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to begin transaction: %v", err)
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE telegram_id = $1`, parentTelegramID).Scan(&parentID)
	if err == sql.ErrNoRows {
		// Вставка родителя в users
		err := tx.QueryRowContext(ctx, `
			INSERT INTO users (telegram_id, name, role, confirmed)
			VALUES ($1, $2, 'parent', FALSE)
			ON CONFLICT (telegram_id) DO UPDATE
			SET name = EXCLUDED.name, role = 'parent'
			RETURNING id
		`, parentTelegramID, parentName).Scan(&parentID)
		if err != nil {
			log.Printf("[PARENT_ERROR] upsert parent failed: %v", err)
			_ = tx.Rollback()
			return 0, err
		}
	} else if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	// Привязка к ученику
	_, err = tx.ExecContext(ctx, `INSERT INTO parents_students (parent_id, student_id) VALUES ($1, $2)`, parentID, studentID)
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to insert into parents_students: %v", err)
		_ = tx.Rollback()
		return 0, err
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("[PARENT_ERROR] failed to commit transaction: %v", err)
		return 0, err
	}

	// Родитель мог быть неактивным — сразу пересчитаем активность:
	// если теперь у него есть хотя бы один активный ребёнок, он станет активным.
	if err := db.RefreshParentActiveFlag(ctx, database, parentID); err != nil {
		log.Printf("[PARENT_ERROR] refresh parent activity failed: %v", err)
	}

	log.Printf("[PARENT_SUCCESS] linked parent (tg_id=%d) to student_id=%d", parentTelegramID, studentID)
	return parentID, nil
}
