package handlers

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/export"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type exportUsersState struct {
	MessageID       int
	IncludeInactive bool
	Step            int // 1=экран параметров → 2=генерация
}

var expUsers = map[int64]*exportUsersState{}

const (
	cbEUOpen     = "exp_users_open"
	cbEUToggle   = "exp_users_toggle"
	cbEUGenerate = "exp_users_gen"
	cbEUCancel   = "exp_users_cancel"
	cbEUBack     = "exp_users_back" // = cancel -> в меню экспорта
)

func StartExportUsers(bot *tgbotapi.BotAPI, _ *sql.DB, msg *tgbotapi.Message, isAdmin bool) {
	chatID := msg.Chat.ID
	st := &exportUsersState{IncludeInactive: false, Step: 1}
	expUsers[chatID] = st

	text := "Экспорт → Пользователи\n\nСформировать Excel со вкладками: Все, Учителя, Администрация, Ученики, Родители."
	var rows [][]tgbotapi.InlineKeyboardButton
	if isAdmin {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(toggleLabel(st.IncludeInactive), cbEUToggle),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📤 Сформировать", cbEUGenerate),
	))
	rows = append(rows, fsmutil.BackCancelRow(cbEUBack, cbEUCancel))

	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	out := tgbotapi.NewMessage(chatID, text)
	out.ReplyMarkup = mk
	sent, _ := bot.Send(out)
	st.MessageID = sent.MessageID
}

func HandleExportUsersCallback(bot *tgbotapi.BotAPI, database *sql.DB, cb *tgbotapi.CallbackQuery, isAdmin bool) {
	chatID := cb.Message.Chat.ID
	state := expUsers[chatID]
	if state == nil {
		return
	}
	_, _ = bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	switch cb.Data {
	case cbEUCancel, cbEUBack:
		// гасим клавиатуру и сообщаем
		disable := tgbotapi.NewEditMessageReplyMarkup(chatID, state.MessageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
		bot.Request(disable)
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Отменено."))
		delete(expUsers, chatID)
		return

	case cbEUToggle:
		if !isAdmin {
			return
		}
		state.IncludeInactive = !state.IncludeInactive
		// перерисовать клавиатуру
		rows := [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(toggleLabel(state.IncludeInactive), cbEUToggle)),
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("📤 Сформировать", cbEUGenerate)),
			fsmutil.BackCancelRow(cbEUBack, cbEUCancel),
		}
		mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
		euReplace(
			bot, chatID, state,
			"Экспорт → Пользователи\n\nСформировать Excel со вкладками: Все, Учителя, Администрация, Ученики, Родители.",
			mk,
		)
		return

	case cbEUGenerate:
		state.Step = 2
		euClearMarkup(bot, chatID, state)
		bot.Send(tgbotapi.NewEditMessageText(chatID, state.MessageID, "⏳ Формируем файл…"))

		key := fmt.Sprintf("exp_users:%d", chatID)
		if !fsmutil.SetPending(chatID, key) {
			bot.Send(tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…"))
			return
		}
		includeInactive := state.IncludeInactive

		go func() {
			defer fsmutil.ClearPending(chatID, key)
			defer delete(expUsers, chatID)

			all, err := db.ListAllUsers(database, state.IncludeInactive)
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}
			teachers, err := db.ListTeachers(database, state.IncludeInactive)
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}
			admins, err := db.ListAdministration(database, state.IncludeInactive)
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}
			students, err := db.ListStudents(database, state.IncludeInactive)
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}
			parents, err := db.ListParents(database, state.IncludeInactive)
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}

			var sheets []export.SheetSpec
			// Все
			rowsAll := make([][]string, 0, len(all))
			for _, u := range all {
				class := ""
				if u.ClassNum.Valid && u.ClassLet.Valid {
					class = fmt.Sprintf("%d%s", u.ClassNum.Int64, u.ClassLet.String)
				}
				rowsAll = append(rowsAll, []string{u.Name, u.Role, class})
			}
			sheets = append(sheets, export.SheetSpec{
				Title:  "Все",
				Header: []string{"ФИО", "Роль", "Класс"},
				Rows:   rowsAll,
			})
			// Учителя
			rowsT := make([][]string, 0, len(teachers))
			for _, n := range teachers {
				rowsT = append(rowsT, []string{n})
			}
			sheets = append(sheets, export.SheetSpec{Title: "Учителя", Header: []string{"ФИО"}, Rows: rowsT})
			// Администрация
			rowsA := make([][]string, 0, len(admins))
			for _, n := range admins {
				rowsA = append(rowsA, []string{n})
			}
			sheets = append(sheets, export.SheetSpec{Title: "Администрация", Header: []string{"ФИО"}, Rows: rowsA})
			// Ученики
			rowsS := make([][]string, 0, len(students))
			for _, s := range students {
				class := ""
				if s.ClassNum.Valid && s.ClassLet.Valid {
					class = fmt.Sprintf("%d%s", s.ClassNum.Int64, s.ClassLet.String)
				}
				rowsS = append(rowsS, []string{s.Name, class, s.ParentsCSV})
			}
			sheets = append(sheets, export.SheetSpec{Title: "Ученики", Header: []string{"ФИО", "Класс", "Родители"}, Rows: rowsS})
			// Родители
			rowsP := make([][]string, 0, len(parents))
			for _, p := range parents {
				rowsP = append(rowsP, []string{p.ParentName, p.Children, p.Classes})
			}
			sheets = append(sheets, export.SheetSpec{Title: "Родители", Header: []string{"Родитель", "Дети", "Классы"}, Rows: rowsP})

			wb, err := export.NewUsersWorkbook(sheets)
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}
			path, err := wb.SaveTemp()
			if err != nil {
				fail(bot, chatID, state, err)
				return
			}

			doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(path))
			doc.Caption = captionInactive(includeInactive)
			if _, err := bot.Send(doc); err != nil {
				log.Printf("[EXPORT_USERS] send failed: %v", err)
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось отправить файл."))
				return
			}
		}()
		return
	}
}

func toggleLabel(on bool) string {
	if on {
		return "◻️ Неактивные: ВКЛ"
	}
	return "◼️ Неактивные: ВЫКЛ"
}
func captionInactive(on bool) string {
	if on {
		return "Включены неактивные"
	}
	return "Только активные"
}
func fail(bot *tgbotapi.BotAPI, chatID int64, st *exportUsersState, err error) {
	log.Printf("[EXPORT_USERS] %v", err)
	bot.Send(tgbotapi.NewEditMessageText(chatID, st.MessageID, "❌ Ошибка при формировании экспорта."))
	delete(expUsers, chatID)
}

// Отправить новое сообщение и удалить старое → гарантированная перерисовка клавиатуры
func euReplace(bot *tgbotapi.BotAPI, chatID int64, st *exportUsersState, text string, mk tgbotapi.InlineKeyboardMarkup) {
	if st.MessageID != 0 {
		_, _ = bot.Request(tgbotapi.NewDeleteMessage(chatID, st.MessageID))
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = mk
	sent, _ := bot.Send(msg)
	st.MessageID = sent.MessageID
}

// Просто погасить клавиатуру у текущего сообщения (чтоб не было дабл-кликов)
func euClearMarkup(bot *tgbotapi.BotAPI, chatID int64, state *exportUsersState) {
	if state.MessageID == 0 {
		return
	}
	_, _ = bot.Request(tgbotapi.NewEditMessageReplyMarkup(chatID, state.MessageID,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}))
}
