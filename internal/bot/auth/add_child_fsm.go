package auth

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/handlers"
	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	StateAddChildName        = "add_child_name"
	StateAddChildClassNumber = "add_child_class_number"
	StateAddChildClassLetter = "add_child_class_letter"
	StateAddChildWaiting     = "add_child_waiting"
)

var addChildFSM = make(map[int64]string)
var addChildData = make(map[int64]*ParentRegisterData)

// ===== helpers (как в export/add/remove) =====

func addChildBackCancelRow() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("add_child_back", "add_child_cancel")
}

func addChildClassNumberRows() [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 11; i++ {
		cb := fmt.Sprintf("parent_class_num_%d", i) // оставим префикс parent_* чтобы не плодить новые
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d класс", i), cb),
		))
	}
	rows = append(rows, addChildBackCancelRow())
	return rows
}

func addChildClassLetterRows() [][]tgbotapi.InlineKeyboardButton {
	letters := []string{"А", "Б", "В", "Г", "Д"}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range letters {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(l, "parent_class_letter_"+l),
		))
	}
	rows = append(rows, addChildBackCancelRow())
	return rows
}

func addChildEditMenu(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	bot.Send(cfg)
}

// ===== Старт/текстовые шаги =====

func StartAddChild(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	addChildFSM[chatID] = StateAddChildName
	addChildData[chatID] = &ParentRegisterData{}
	bot.Send(tgbotapi.NewMessage(chatID, "Введите ФИО ребёнка, которого хотите добавить:\n(или напишите Отмена)"))
}

func HandleAddChildText(bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	state := GetAddChildFSMState(chatID)

	trimmed := strings.TrimSpace(msg.Text)
	if strings.EqualFold(trimmed, "отмена") || strings.EqualFold(trimmed, "/cancel") {
		delete(addChildFSM, chatID)
		delete(addChildData, chatID)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Добавление ребёнка отменено."))
		return
	}

	switch state {
	case StateAddChildName:
		fio := strings.TrimSpace(msg.Text)
		fio = db.ToTitleRU(fio)

		addChildData[chatID].StudentName = fio
		addChildFSM[chatID] = StateAddChildClassNumber

		out := tgbotapi.NewMessage(chatID, "Выберите номер класса ребёнка:")
		out.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(addChildClassNumberRows()...)
		bot.Send(out)
	case StateAddChildClassNumber:
		// доп. защита, если пользователь ввёл цифрой
		number, err := strconv.Atoi(msg.Text)
		if err != nil || number < 1 || number > 11 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите корректный номер класса (1–11) или используйте кнопки."))
			return
		}
		addChildData[chatID].ClassNumber = number
		addChildFSM[chatID] = StateAddChildClassLetter
		// создадим карточку выбора буквы
		out := tgbotapi.NewMessage(chatID, "Выберите букву класса:")
		out.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(addChildClassLetterRows()...)
		bot.Send(out)
	case StateAddChildClassLetter:
		// если пришёл текст буквы (но лучше кнопкой)
		letter := strings.ToUpper(strings.TrimSpace(msg.Text))
		if letter == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Укажите букву класса или используйте кнопки."))
			return
		}
		handleAddChildFinish(bot, database, chatID, msg.MessageID, letter)
	}
}

func GetAddChildFSMState(chatID int64) string {
	return addChildFSM[chatID]
}

// ===== Коллбеки (ЕДИНАЯ точка) =====

func HandleAddChildCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	messageID := cb.Message.MessageID
	data := cb.Data
	state := GetAddChildFSMState(chatID)

	// Отмена
	if data == "add_child_cancel" {
		delete(addChildFSM, chatID)
		delete(addChildData, chatID)
		fsmutil.DisableMarkup(bot, chatID, messageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, "🚫 Добавление ребёнка отменено."))
		return
	}
	// Назад
	if data == "add_child_back" {
		switch state {
		case StateAddChildClassNumber:
			// назад к ФИО — карточку гасим
			fsmutil.DisableMarkup(bot, chatID, messageID)
			bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, "Введите ФИО ребёнка, которого хотите добавить:\n(или напишите Отмена)"))
			addChildFSM[chatID] = StateAddChildName
		case StateAddChildClassLetter:
			addChildFSM[chatID] = StateAddChildClassNumber
			addChildEditMenu(bot, chatID, messageID, "Выберите номер класса ребёнка:", addChildClassNumberRows())
		case StateAddChildWaiting:
			bot.Request(tgbotapi.NewCallback(cb.ID, "Заявка уже отправлена, ожидайте подтверждения."))
		default:
			bot.Request(tgbotapi.NewCallback(cb.ID, "Действие недоступно на этом шаге."))
		}
		return
	}

	// Выбор номера
	if strings.HasPrefix(data, "parent_class_num_") && (state == StateAddChildClassNumber || state == StateAddChildName) {
		numStr := strings.TrimPrefix(data, "parent_class_num_")
		num, err := strconv.Atoi(numStr)
		if err != nil || num < 1 || num > 11 {
			bot.Send(tgbotapi.NewCallback(cb.ID, "Неверный номер класса"))
			return
		}
		addChildData[chatID].ClassNumber = num
		addChildFSM[chatID] = StateAddChildClassLetter
		addChildEditMenu(bot, chatID, messageID, "Выберите букву класса:", addChildClassLetterRows())
		return
	}

	// Выбор буквы
	if strings.HasPrefix(data, "parent_class_letter_") && state == StateAddChildClassLetter {
		letter := strings.TrimPrefix(data, "parent_class_letter_")
		handleAddChildFinish(bot, database, chatID, messageID, letter)
		return
	}
}

// ===== Завершение: создаём заявку на привязку, а не сразу связь =====

func handleAddChildFinish(bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, messageID int, letter string) {
	addChildData[chatID].ClassLetter = letter
	addChildFSM[chatID] = StateAddChildWaiting

	// Находим ученика
	studentID, err := FindStudentID(database, &ParentRegisterData{
		StudentName: addChildData[chatID].StudentName,
		ClassNumber: addChildData[chatID].ClassNumber,
		ClassLetter: addChildData[chatID].ClassLetter,
	})

	fmt.Println()
	log.Printf("Файл add_child_fsm: %d", studentID)
	fmt.Println()

	if err != nil {
		fsmutil.DisableMarkup(bot, chatID, messageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, "❌ Ученик не найден. Введите ФИО заново:"))
		addChildFSM[chatID] = StateAddChildName
		return
	}

	// Создаём заявку на привязку (ожидает подтверждения админом)
	reqID, err := CreateParentLinkRequest(database, chatID, studentID)
	if err != nil {
		fsmutil.DisableMarkup(bot, chatID, messageID)
		bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, "❌ Ошибка при создании заявки. Попробуйте позже."))
		delete(addChildFSM, chatID)
		delete(addChildData, chatID)
		return
	}

	// Уведомляем админов о новой заявке (отдельные кнопки подтверждения/отклонения)
	handlers.NotifyAdminsAboutParentLink(bot, database, reqID)

	fsmutil.DisableMarkup(bot, chatID, messageID)
	bot.Send(tgbotapi.NewEditMessageText(chatID, messageID, "📨 Заявка на добавление ребёнка отправлена администратору. Ожидайте подтверждения."))
	delete(addChildFSM, chatID)
	delete(addChildData, chatID)
}

// ===== База/заявки =====

// CreateParentLinkRequest создаёт запись-заявку на привязку родителя и ребёнка.
// Требуется таблица parent_link_requests(id, parent_id, student_id, created_at) с автоинкрементом.
func CreateParentLinkRequest(database *sql.DB, parentTelegramID int64, studentID int) (int64, error) {
	// находим parent_id по telegram_id
	var parentID int64
	err := database.QueryRow(`SELECT id FROM users WHERE telegram_id = ? AND role = 'parent' AND confirmed = 1`, parentTelegramID).Scan(&parentID)
	if err != nil {
		return 0, fmt.Errorf("родитель не найден/не подтверждён: %w", err)
	}

	// создаём заявку
	res, err := database.Exec(`
		INSERT INTO parent_link_requests (parent_id, student_id, created_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, parentID, studentID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
