package app

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

// ====== STATE ======

type teacherFSMState struct {
	Step       int
	Weekday    time.Weekday
	Start      time.Time // фиктивная дата, время важно
	End        time.Time
	StepMin    int
	ClassID    int64
	MsgID      int // последний message id, который редактируем
	TempNumber int // выбранный номер класса (для шага выбора буквы)
}

var teacherFSM sync.Map // key: chatID(int64) -> *teacherFSMState

func getTeacherFSM(chatID int64) (*teacherFSMState, bool) {
	v, ok := teacherFSM.Load(chatID)
	if !ok {
		return nil, false
	}
	return v.(*teacherFSMState), true
}
func setTeacherFSM(chatID int64, st *teacherFSMState) { teacherFSM.Store(chatID, st) }
func clearTeacherFSM(chatID int64)                    { teacherFSM.Delete(chatID) }

// ====== UI helpers ======

func upsertStepMsg(bot *tgbotapi.BotAPI, chatID int64, st *teacherFSMState, text string, kb *tgbotapi.InlineKeyboardMarkup) {
	if st.MsgID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		if kb != nil {
			msg.ReplyMarkup = kb
		}
		out, _ := bot.Send(msg)
		st.MsgID = out.MessageID
		return
	}
	edit := tgbotapi.NewEditMessageText(chatID, st.MsgID, text)
	if kb != nil {
		edit.ReplyMarkup = kb
	}
	_, _ = bot.Send(edit)
}

func kbRow(btns ...tgbotapi.InlineKeyboardButton) []tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardRow(btns...)
}

func wdBtn(title string, wd time.Weekday) tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("t_slots:day:%d", int(wd)))
}

// ====== ENTRY ======

func TryHandleTeacherSlotsCommand(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" || !strings.HasPrefix(msg.Text, "/t_slots") {
		return false
	}
	u, _ := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if u == nil || u.Role == nil || *u.Role != models.Teacher {
		_, _ = bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Эта команда доступна только учителям."))
		return true
	}
	clearTeacherFSM(msg.Chat.ID)
	st := &teacherFSMState{Step: 1}
	setTeacherFSM(msg.Chat.ID, st)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		kbRow(wdBtn("Пн", time.Monday), wdBtn("Вт", time.Tuesday), wdBtn("Ср", time.Wednesday)),
		kbRow(wdBtn("Чт", time.Thursday), wdBtn("Пт", time.Friday), wdBtn("Сб", time.Saturday)),
		kbRow(wdBtn("Вс", time.Sunday), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
	)
	upsertStepMsg(bot, msg.Chat.ID, st, "Шаг 1/5. Выберите день недели:", &kb)
	return true
}

// ====== CALLBACKS ======

func TryHandleTeacherSlotsCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery) bool {
	if cb == nil || cb.Data == "" || !strings.HasPrefix(cb.Data, "t_slots:") {
		return false
	}
	chatID := cb.Message.Chat.ID
	st, ok := getTeacherFSM(chatID)
	if !ok {
		st = &teacherFSMState{Step: 1}
		setTeacherFSM(chatID, st)
	}
	st.MsgID = cb.Message.MessageID // редактируем текущее сообщение

	parts := strings.Split(cb.Data, ":")
	switch parts[1] {
	case "cancel":
		clearTeacherFSM(chatID)
		edit := tgbotapi.NewEditMessageText(chatID, st.MsgID, "Отменено.")
		_, _ = bot.Send(edit)
		return true

	case "day":
		wdNum, _ := strconv.Atoi(parts[2])
		st.Weekday = time.Weekday(wdNum)
		st.Step = 2
		setTeacherFSM(chatID, st)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:1"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
		)
		upsertStepMsg(bot, chatID, st, "Шаг 2/5. Введите временное окно в формате HH:MM-HH:MM (например, 16:00-18:00)", &kb)
		return true

	case "back":
		step, _ := strconv.Atoi(parts[2])
		switch step {
		case 1:
			st.Step = 1
			setTeacherFSM(chatID, st)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(wdBtn("Пн", time.Monday), wdBtn("Вт", time.Tuesday), wdBtn("Ср", time.Wednesday)),
				kbRow(wdBtn("Чт", time.Thursday), wdBtn("Пт", time.Friday), wdBtn("Сб", time.Saturday)),
				kbRow(wdBtn("Вс", time.Sunday), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
			)
			upsertStepMsg(bot, chatID, st, "Шаг 1/5. Выберите день недели:", &kb)
			return true
		case 2:
			st.Step = 2
			setTeacherFSM(chatID, st)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:1"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
			)
			upsertStepMsg(bot, chatID, st, "Шаг 2/5. Введите временное окно в формате HH:MM-HH:MM (например, 16:00-18:00)", &kb)
			return true
		case 3:
			st.Step = 3
			setTeacherFSM(chatID, st)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:2"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
			)
			upsertStepMsg(bot, chatID, st, "Шаг 3/5. Введите шаг в минутах (например, 20)", &kb)
			return true
		case 4:
			// вернуться к выбору номера
			st.Step = 4
			setTeacherFSM(chatID, st)
			showClassNumberMenu(ctx, bot, database, chatID, st)
			return true
		}
		return true

	case "cnum": // выбор номера класса
		num, _ := strconv.Atoi(parts[2])
		st.TempNumber = num
		st.Step = 5
		setTeacherFSM(chatID, st)
		showClassLetterMenu(ctx, bot, database, chatID, st)
		return true

	case "csel": // выбор конкретного класса id
		cid, _ := strconv.ParseInt(parts[2], 10, 64)
		st.ClassID = cid
		// готово — генерим слоты за 1 неделю
		loc := time.Local
		starts := generateStartsWeeks(st.Weekday, st.Start, st.End, time.Duration(st.StepMin)*time.Minute, 1, loc)
		if len(starts) == 0 {
			upsertStepMsg(bot, chatID, st, "Окно времени пустое — слоты не созданы.", nil)
			clearTeacherFSM(chatID)
			return true
		}
		utc := make([]time.Time, 0, len(starts))
		for _, lt := range starts {
			utc = append(utc, lt.UTC())
		}

		u, _ := db.GetUserByTelegramID(ctx, database, chatID)
		inserted, err := db.CreateSlots(ctx, database, u.ID, st.ClassID, utc, time.Duration(st.StepMin)*time.Minute)
		if err != nil {
			upsertStepMsg(bot, chatID, st, "Ошибка при создании слотов.", nil)
			clearTeacherFSM(chatID)
			return true
		}
		upsertStepMsg(bot, chatID, st, fmt.Sprintf("Готово. Создано слотов: %d (дубли проигнорированы).", inserted), nil)
		clearTeacherFSM(chatID)
		return true
	}
	return false
}

// ====== TEXT STEPS ======

func TryHandleTeacherSlotsText(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	st, ok := getTeacherFSM(msg.Chat.ID)
	if !ok {
		return false
	}
	switch st.Step {
	case 2:
		startT, endT, ok := parseTimeWindow(strings.TrimSpace(msg.Text))
		if !ok || !startT.Before(endT) {
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:1"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
			)
			upsertStepMsg(bot, msg.Chat.ID, st, "Неверный формат. Пример: 16:00-18:00", &kb)
			return true
		}
		st.Start, st.End = startT, endT
		st.Step = 3
		setTeacherFSM(msg.Chat.ID, st)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:2"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
		)
		upsertStepMsg(bot, msg.Chat.ID, st, "Шаг 3/5. Введите шаг в минутах (например, 20)", &kb)
		return true

	case 3:
		stepMin, err := strconv.Atoi(strings.TrimSpace(msg.Text))
		if err != nil || stepMin <= 0 {
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:2"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
			)
			upsertStepMsg(bot, msg.Chat.ID, st, "Шаг должен быть положительным числом минут.", &kb)
			return true
		}
		st.StepMin = stepMin
		st.Step = 4
		setTeacherFSM(msg.Chat.ID, st)
		showClassNumberMenu(ctx, bot, database, msg.Chat.ID, st)
		return true
	}
	return false
}

// ====== CLASS MENUS ======

func showClassNumberMenu(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, st *teacherFSMState) {
	nums, err := db.ListClassNumbers(ctx, database)
	if err != nil || len(nums) == 0 {
		upsertStepMsg(bot, chatID, st, "Классы не найдены.", nil)
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, n := range nums {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d", n), fmt.Sprintf("t_slots:cnum:%d", n)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:3"),
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	upsertStepMsg(bot, chatID, st, "Шаг 4/5. Выберите номер класса:", &kb)
}

func showClassLetterMenu(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, chatID int64, st *teacherFSMState) {
	cls, err := db.ListClassesByNumber(ctx, database, st.TempNumber)
	if err != nil || len(cls) == 0 {
		upsertStepMsg(bot, chatID, st, "Буквы для выбранного номера не найдены.", nil)
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range cls {
		title := fmt.Sprintf("%d%s", c.Number, strings.ToUpper(c.Letter))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("t_slots:csel:%d", c.ID)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:4"),
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	upsertStepMsg(bot, chatID, st, "Шаг 5/5. Выберите букву класса:", &kb)
}
