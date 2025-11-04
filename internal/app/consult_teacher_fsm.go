package app

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

// ====== STATE ======

type teacherFSMState struct {
	Step             int
	Day              time.Time
	Weekday          time.Weekday
	Start            time.Time // фиктивная дата, время важно
	End              time.Time
	StepMin          int
	ClassID          int64
	MsgID            int // последний message id, который редактируем
	TempNumber       int // выбранный номер класса (для шага выбора буквы)
	SelectedClassIDs []int64
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
		out, _ := tg.Send(bot, msg)
		st.MsgID = out.MessageID
		return
	}
	edit := tgbotapi.NewEditMessageText(chatID, st.MsgID, text)
	if kb != nil {
		edit.ReplyMarkup = kb
	}
	if _, err := tg.Send(bot, edit); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func kbRow(btns ...tgbotapi.InlineKeyboardButton) []tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardRow(btns...)
}

// ====== ENTRY ======

func TryHandleTeacherSlotsCommand(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Text == "" || !strings.HasPrefix(msg.Text, "/t_slots") {
		return false
	}
	u, _ := db.GetUserByTelegramID(ctx, database, msg.Chat.ID)
	if u == nil || u.Role == nil || *u.Role != models.Teacher {
		if _, err := tg.Send(bot, tgbotapi.NewMessage(msg.Chat.ID, "Эта команда доступна только учителям.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true
	}
	clearTeacherFSM(msg.Chat.ID)
	st := &teacherFSMState{Step: 1}
	setTeacherFSM(msg.Chat.ID, st)

	today := time.Now().In(time.Local).Truncate(24 * time.Hour)
	var rows [][]tgbotapi.InlineKeyboardButton
	wd := []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}
	for i := 0; i < 14; i++ {
		d := today.AddDate(0, 0, i)
		label := d.Format("02.01") + " (" + wd[int(d.Weekday())] + ")"
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label, "t_slots:day:"+d.Format("2006-01-02")),
			),
		)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel"),
	))
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	upsertStepMsg(bot, msg.Chat.ID, st, "Шаг 1/5. Выберите дату:", &kb)
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
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return true

	case "day":
		// parts[2] = YYYY-MM-DD
		d, err := time.Parse("2006-01-02", parts[2])
		if err != nil {
			_ = sendCb(bot, cb, "Дата в формате YYYY-MM-DD")
			return true
		}
		st.Day = d.In(time.Local) // поле Day добавим в состояние (см. ниже)
		st.Step = 2
		setTeacherFSM(chatID, st)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			kbRow(
				tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:1"),
				tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel"),
			),
		)
		upsertStepMsg(bot, chatID, st, "Шаг 2/5. Введите временное окно в формате HH:MM-HH:MM (например, 16:00-18:00)", &kb)
		return true

	case "back":
		step, _ := strconv.Atoi(parts[2])
		switch step {
		case 1:
			st.Step = 1
			setTeacherFSM(chatID, st)
			// 14 дат вперёд, подпись: 16.10 (Ср)
			var rows [][]tgbotapi.InlineKeyboardButton
			today := time.Now().In(time.Local).Truncate(24 * time.Hour)
			for i := 0; i < 14; i++ {
				d := today.AddDate(0, 0, i)
				label := d.Format("02.01") + " (" + []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}[int(d.Weekday())] + ")"
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(label, "t_slots:day:"+d.Format("2006-01-02")),
				))
			}
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")))
			kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
			upsertStepMsg(bot, chatID, st, "Шаг 1/5. Выберите дату:", &kb)
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
			upsertStepMsg(bot, chatID, st, "Шаг 3/5. Введите шаг в минутах, необходимый на одну консультацию (например, 15)", &kb)
			return true
		case 4:
			st.Step = 3
			setTeacherFSM(chatID, st)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(
					tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:2"),
					tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel"),
				),
			)
			nextStepBelowInput(bot, chatID, st, "Шаг 3/5. Введите шаг в минутах, необходимый на одну консультацию (например, 15)", &kb)
			return true
		}
		return true

	case "toggle_class": // t_slots:toggle_class:<id>
		cid, _ := strconv.ParseInt(parts[2], 10, 64)
		// toggle в st.SelectedClassIDs
		found := false
		for i, v := range st.SelectedClassIDs {
			if v == cid {
				st.SelectedClassIDs = append(st.SelectedClassIDs[:i], st.SelectedClassIDs[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			st.SelectedClassIDs = append(st.SelectedClassIDs, cid)
		}
		setTeacherFSM(chatID, st)
		showClassMultiMenu(ctx, bot, database, chatID, st)
		return true

	case "classes_done": // завершить выбор букв
		if len(st.SelectedClassIDs) == 0 {
			_ = sendCb(bot, cb, "Выберите хотя бы один класс")
			return true
		}
		// генерируем слоты сразу здесь
		loc := time.Local
		makeDayTime := func(day time.Time, hh, mm int) time.Time {
			return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
		}
		var starts []time.Time
		step := time.Duration(st.StepMin) * time.Minute
		for w := 0; w < 2; w++ {
			day := st.Day.AddDate(0, 0, 7*w)
			startBound := makeDayTime(day, st.Start.Hour(), st.Start.Minute())
			endBound := makeDayTime(day, st.End.Hour(), st.End.Minute())
			for tm := startBound; !tm.Add(step).After(endBound); tm = tm.Add(step) {
				starts = append(starts, tm)
			}
		}
		if len(starts) == 0 {
			upsertStepMsg(bot, chatID, st, "Окно времени пустое — слоты не созданы.", nil)
			clearTeacherFSM(chatID)
			return true
		}
		u, _ := db.GetUserByTelegramID(ctx, database, chatID)
		inserted, err := db.CreateSlotsMultiClasses(ctx, database, u.ID, st.SelectedClassIDs, starts, st.StepMin)
		if err != nil {
			upsertStepMsg(bot, chatID, st, "Ошибка при создании слотов.", nil)
			clearTeacherFSM(chatID)
			return true
		}
		nextStepBelowInput(bot, chatID, st, fmt.Sprintf("Готово. Создано слотов: %d.", inserted), nil)
		clearTeacherFSM(chatID)
		return true

	case "csel": // выбор конкретного класса id
		cid, _ := strconv.ParseInt(parts[2], 10, 64)
		st.ClassID = cid

		// генерация слотов на выбранную дату и ещё через 7 дней (2 недели)
		loc := time.Local
		makeDayTime := func(day time.Time, hh, mm int) time.Time {
			return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
		}
		var starts []time.Time
		step := time.Duration(st.StepMin) * time.Minute
		for w := 0; w < 2; w++ {
			day := st.Day.AddDate(0, 0, 7*w)
			startBound := makeDayTime(day, st.Start.Hour(), st.Start.Minute())
			endBound := makeDayTime(day, st.End.Hour(), st.End.Minute())
			for tm := startBound; !tm.Add(step).After(endBound); tm = tm.Add(step) {
				starts = append(starts, tm)
			}
		}
		if len(starts) == 0 {
			upsertStepMsg(bot, chatID, st, "Окно времени пустое — слоты не созданы.", nil)
			clearTeacherFSM(chatID)
			return true
		}

		u, _ := db.GetUserByTelegramID(ctx, database, chatID)

		// если мультивыбор классов не делал — используем выбранный один:
		classIDs := st.SelectedClassIDs
		if len(classIDs) == 0 && st.ClassID != 0 {
			classIDs = []int64{st.ClassID}
		}

		inserted, err := db.CreateSlotsMultiClasses(ctx, database, u.ID, classIDs, starts, st.StepMin)
		if err != nil {
			upsertStepMsg(bot, chatID, st, "Ошибка при создании слотов.", nil)
			clearTeacherFSM(chatID)
			return true
		}
		nextStepBelowInput(bot, chatID, st, fmt.Sprintf("Готово. Создано слотов: %d.", inserted), nil)
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
			nextStepBelowInput(bot, msg.Chat.ID, st, "Неверный формат. Пример: 16:00-18:00", &kb)
			return true
		}
		st.Start, st.End = startT, endT
		st.Step = 3
		setTeacherFSM(msg.Chat.ID, st)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:2"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
		)
		nextStepBelowInput(bot, msg.Chat.ID, st, "Шаг 3/5. Введите шаг в минутах, необходимый на одну консультацию (например, 15)", &kb)
		return true

	case 3:
		stepMin, err := strconv.Atoi(strings.TrimSpace(msg.Text))
		if err != nil || stepMin <= 0 {
			kb := tgbotapi.NewInlineKeyboardMarkup(
				kbRow(tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:2"), tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel")),
			)
			nextStepBelowInput(bot, msg.Chat.ID, st, "Шаг должен быть положительным числом минут.", &kb)
			return true
		}
		st.StepMin = stepMin
		st.Step = 4
		setTeacherFSM(msg.Chat.ID, st)
		showClassMultiMenu(ctx, bot, database, msg.Chat.ID, st)
		return true
	}
	return false
}

// ====== CLASS MENUS ======

func showClassMultiMenu(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	database *sql.DB,
	chatID int64,
	st *teacherFSMState,
) {
	classes, err := db.ListVisibleClasses(ctx, database)
	if err != nil || len(classes) == 0 {
		upsertStepMsg(bot, chatID, st, "Классы не найдены.", nil)
		return
	}

	// сделаем быстрый lookup уже выбранных
	selected := make(map[int64]bool, len(st.SelectedClassIDs))
	for _, id := range st.SelectedClassIDs {
		selected[id] = true
	}

	// отсортируем по номеру, потом по букве
	sort.Slice(classes, func(i, j int) bool {
		if classes[i].Number == classes[j].Number {
			return classes[i].Letter < classes[j].Letter
		}
		return classes[i].Number < classes[j].Number
	})

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range classes {
		title := fmt.Sprintf("%d%s", c.Number, strings.ToUpper(c.Letter))
		if selected[c.ID] {
			title = "✅ " + title
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("t_slots:toggle_class:%d", c.ID)),
		))
	}

	// нижние кнопки
	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "t_slots:classes_done"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Назад", "t_slots:back:3"),
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "t_slots:cancel"),
		),
	)

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	upsertStepMsg(bot, chatID, st, "Шаг 4/5. Выберите один или несколько классов:", &kb)
}

// nextStepBelowInput отправить следующий шаг ниже пользовательского ввода: удалить старое бот-сообщение и прислать новое
func nextStepBelowInput(bot *tgbotapi.BotAPI, chatID int64, st *teacherFSMState, text string, kb *tgbotapi.InlineKeyboardMarkup) {
	if st.MsgID != 0 {
		_, _ = tg.Request(bot, tgbotapi.NewDeleteMessage(chatID, st.MsgID))
		st.MsgID = 0
	}
	msg := tgbotapi.NewMessage(chatID, text)
	if kb != nil {
		msg.ReplyMarkup = kb
	}
	out, _ := tg.Send(bot, msg)
	st.MsgID = out.MessageID
}
