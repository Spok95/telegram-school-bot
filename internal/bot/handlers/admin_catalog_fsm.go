package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Spok95/telegram-school-bot/internal/bot/shared/fsmutil"
	"github.com/Spok95/telegram-school-bot/internal/db"
	"github.com/Spok95/telegram-school-bot/internal/metrics"
	"github.com/Spok95/telegram-school-bot/internal/tg"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type CatalogFSMState struct {
	Step           int
	CategoryID     *int64
	LevelID        *int64
	Awaiting       string
	TempLevelValue *int
}

var catalogStates = map[int64]*CatalogFSMState{}

// ====== helpers

func catBackCancel() []tgbotapi.InlineKeyboardButton {
	return fsmutil.BackCancelRow("catalog_back", "catalog_cancel")
}

func editTextAndMarkup(bot *tgbotapi.BotAPI, chatID int64, msgID int, text string, rows [][]tgbotapi.InlineKeyboardButton) {
	cfg := tgbotapi.NewEditMessageText(chatID, msgID, text)
	mk := tgbotapi.NewInlineKeyboardMarkup(rows...)
	cfg.ReplyMarkup = &mk
	if _, err := tg.Send(bot, cfg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func mark(b bool) string {
	if b {
		return "✅"
	}
	return "🚫"
}

// ====== start

func StartCatalogFSM(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	catalogStates[chatID] = &CatalogFSMState{Step: 1}
	showCategoriesList(ctx, bot, chatID, 0, false, database)
}

func GetCatalogState(userID int64) *CatalogFSMState {
	return catalogStates[userID]
}

// ====== UI builders

func showCategoriesList(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, messageID int, edit bool, database *sql.DB) {
	cats, _ := db.GetCategories(ctx, database, true)

	text := "🗂 Справочники → Категории"
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range cats {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s %s", c.Name, mark(c.IsActive)), fmt.Sprintf("catalog_cat_open_%d", c.ID)),
		)
		rows = append(rows, row)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить категорию", "catalog_cat_add")))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "catalog_cancel")))

	if edit && messageID != 0 {
		editTextAndMarkup(bot, chatID, messageID, text, rows)
		return
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := tg.Send(bot, msg); err != nil {
		metrics.HandlerErrors.Inc()
	}
}

func showCategoryCard(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, messageID int, catID int64, database *sql.DB) {
	c, _ := db.GetCategoryByID(ctx, database, catID)
	text := fmt.Sprintf("📁 Категория: %s %s", c.Name, mark(c.IsActive))
	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Переименовать", fmt.Sprintf("catalog_cat_rename_%d", c.ID)),
			tgbotapi.NewInlineKeyboardButtonData(
				map[bool]string{true: "👁️ Скрыть", false: "👁️ Показать"}[c.IsActive],
				fmt.Sprintf("catalog_cat_toggle_%d", c.ID),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📶 Уровни", fmt.Sprintf("catalog_levels_%d", c.ID)),
		),
		catBackCancel(),
	}
	editTextAndMarkup(bot, chatID, messageID, text, rows)
}

func showLevels(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, messageID int, catID int64, database *sql.DB) {
	c, _ := db.GetCategoryByID(ctx, database, catID)
	levels, _ := db.GetLevelsByCategoryIDFull(ctx, database, catID, true)

	text := fmt.Sprintf("📶 Уровни категории «%s»", c.Name)

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, l := range levels {
		label := fmt.Sprintf("%s (%d) %s", l.Label, l.Value, mark(l.IsActive))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("catalog_lvl_open_%d", l.ID)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить уровень", fmt.Sprintf("catalog_lvl_add_%d", catID)),
	))
	rows = append(rows, catBackCancel())

	editTextAndMarkup(bot, chatID, messageID, text, rows)
}

func showLevelCard(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, messageID int, levelID int64, database *sql.DB) {
	l, _ := db.GetLevelByID(ctx, database, int(levelID))
	text := fmt.Sprintf("🔢 Уровень: %s (%d) %s", l.Label, l.Value, mark(l.IsActive))
	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Переименовать", fmt.Sprintf("catalog_lvl_rename_%d", l.ID)),
			tgbotapi.NewInlineKeyboardButtonData(
				map[bool]string{true: "👁️ Скрыть", false: "👁️ Показать"}[l.IsActive],
				fmt.Sprintf("catalog_lvl_toggle_%d", l.ID),
			),
		),
		catBackCancel(),
	}
	editTextAndMarkup(bot, chatID, messageID, text, rows)
}

// ====== callbacks

func HandleCatalogCallback(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	st := catalogStates[chatID]
	if st == nil {
		return
	}
	data := cq.Data

	// Отмена
	if data == "catalog_cancel" {
		delete(catalogStates, chatID)
		fsmutil.DisableMarkup(bot, chatID, cq.Message.MessageID)
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, "🚫 Справочники: отменено.")
		if _, err := tg.Send(bot, edit); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	// Назад
	if data == "catalog_back" {
		// если были в карточках — возвращаемся на список
		st.Awaiting = ""
		st.LevelID = nil
		st.CategoryID = nil
		showCategoriesList(ctx, bot, chatID, cq.Message.MessageID, true, database)
		return
	}

	switch {
	// список категорий
	case data == "catalog_cat_add":
		st.Awaiting = "cat_name"
		rows := [][]tgbotapi.InlineKeyboardButton{catBackCancel()}
		editTextAndMarkup(bot, chatID, cq.Message.MessageID, "✏️ Введите название новой категории:", rows)

	case strings.HasPrefix(data, "catalog_cat_open_"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_cat_open_"), 10, 64)
		st.CategoryID = &id
		showCategoryCard(ctx, bot, chatID, cq.Message.MessageID, id, database)

	case strings.HasPrefix(data, "catalog_cat_toggle_"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_cat_toggle_"), 10, 64)
		c, _ := db.GetCategoryByID(ctx, database, id)
		_ = db.SetCategoryActive(ctx, database, id, !c.IsActive)
		showCategoryCard(ctx, bot, chatID, cq.Message.MessageID, id, database)

	case strings.HasPrefix(data, "catalog_cat_rename_"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_cat_rename_"), 10, 64)
		st.CategoryID = &id
		st.Awaiting = "cat_rename"
		rows := [][]tgbotapi.InlineKeyboardButton{catBackCancel()}
		editTextAndMarkup(bot, chatID, cq.Message.MessageID, "✏️ Введите новое имя категории:", rows)

	case strings.HasPrefix(data, "catalog_levels_"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_levels_"), 10, 64)
		st.CategoryID = &id
		showLevels(ctx, bot, chatID, cq.Message.MessageID, id, database)

	// уровни
	case strings.HasPrefix(data, "catalog_lvl_add_"):
		catID, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_lvl_add_"), 10, 64)
		st.CategoryID = &catID
		st.Awaiting = "level_value"
		st.TempLevelValue = nil
		rows := [][]tgbotapi.InlineKeyboardButton{catBackCancel()}
		editTextAndMarkup(bot, chatID, cq.Message.MessageID, "✏️ Введите числовое значение уровня (например, 100/200/300):", rows)

	case strings.HasPrefix(data, "catalog_lvl_open_"):
		lvlID, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_lvl_open_"), 10, 64)
		st.LevelID = &lvlID
		showLevelCard(ctx, bot, chatID, cq.Message.MessageID, lvlID, database)

	case strings.HasPrefix(data, "catalog_lvl_toggle_"):
		lvlID, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_lvl_toggle_"), 10, 64)
		l, _ := db.GetLevelByID(ctx, database, int(lvlID))
		_ = db.SetLevelActive(ctx, database, lvlID, !l.IsActive)
		showLevelCard(ctx, bot, chatID, cq.Message.MessageID, lvlID, database)

	case strings.HasPrefix(data, "catalog_lvl_rename_"):
		lvlID, _ := strconv.ParseInt(strings.TrimPrefix(data, "catalog_lvl_rename_"), 10, 64)
		st.LevelID = &lvlID
		st.Awaiting = "level_label_edit"
		rows := [][]tgbotapi.InlineKeyboardButton{catBackCancel()}
		editTextAndMarkup(bot, chatID, cq.Message.MessageID, "✏️ Введите новое имя (label) для уровня:", rows)
	}
}

// ====== text

func HandleCatalogText(ctx context.Context, bot *tgbotapi.BotAPI, database *sql.DB, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	st := catalogStates[chatID]
	if st == nil {
		return
	}

	// текстовая отмена
	if fsmutil.IsCancelText(msg.Text) {
		delete(catalogStates, chatID)
		if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "🚫 Справочники: отменено.")); err != nil {
			metrics.HandlerErrors.Inc()
		}
		return
	}

	switch st.Awaiting {
	case "cat_name":
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Имя не может быть пустым. Введите ещё раз или отправьте «отмена».")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		key := fmt.Sprintf("catalog:addcat:%d", chatID)
		if !fsmutil.SetPending(chatID, key) {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		defer fsmutil.ClearPending(chatID, key)

		if _, err := db.CreateCategory(ctx, database, name, name); err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Ошибка создания категории (возможно, дубликат).")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		st.Awaiting = ""
		showCategoriesList(ctx, bot, chatID, msg.MessageID, false, database)

	case "cat_rename":
		if st.CategoryID == nil {
			return
		}
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Имя не может быть пустым. Введите ещё раз.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		key := fmt.Sprintf("catalog:renamecat:%d", chatID)
		if !fsmutil.SetPending(chatID, key) {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		defer fsmutil.ClearPending(chatID, key)

		_ = db.RenameCategory(ctx, database, *st.CategoryID, name)
		// вернём карточку
		showCategoryCard(ctx, bot, chatID, msg.MessageID-1, *st.CategoryID, database) // -1: текст пришёл отдельным msg

	case "level_value":
		val, err := strconv.Atoi(strings.TrimSpace(msg.Text))
		if err != nil || val <= 0 {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Неверное значение. Введите число (например, 100/200/300).")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		st.TempLevelValue = &val
		st.Awaiting = "level_label"
		rows := [][]tgbotapi.InlineKeyboardButton{catBackCancel()}
		msgOut := tgbotapi.NewMessage(chatID, "✏️ Введите название уровня (label), например «Базовый/Средний/Высокий».")
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		if _, err := tg.Send(bot, msgOut); err != nil {
			metrics.HandlerErrors.Inc()
		}

	case "level_label":
		if st.CategoryID == nil || st.TempLevelValue == nil {
			return
		}
		label := strings.TrimSpace(msg.Text)
		if label == "" {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Название не может быть пустым. Введите ещё раз.")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		key := fmt.Sprintf("catalog:addlevel:%d", chatID)
		if !fsmutil.SetPending(chatID, key) {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		defer fsmutil.ClearPending(chatID, key)

		if _, err := db.CreateLevel(ctx, database, *st.CategoryID, *st.TempLevelValue, label); err != nil {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "❌ Ошибка добавления уровня (возможно, такой value уже есть в категории).")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		st.Awaiting = ""
		showLevels(ctx, bot, chatID, msg.MessageID-1, *st.CategoryID, database)

	case "level_label_edit":
		if st.LevelID == nil {
			return
		}
		label := strings.TrimSpace(msg.Text)
		if label == "" {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⚠️ Название не может быть пустым. Введите ещё раз или «отмена».")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		key := fmt.Sprintf("catalog:renamelevel:%d", chatID)
		if !fsmutil.SetPending(chatID, key) {
			if _, err := tg.Send(bot, tgbotapi.NewMessage(chatID, "⏳ Запрос уже обрабатывается…")); err != nil {
				metrics.HandlerErrors.Inc()
			}
			return
		}
		defer fsmutil.ClearPending(chatID, key)

		_ = db.RenameLevel(ctx, database, *st.LevelID, label)
		showLevelCard(ctx, bot, chatID, msg.MessageID-1, *st.LevelID, database)
	}
}
