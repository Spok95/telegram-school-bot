package db

import (
	"database/sql"
	"fmt"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

func SeedScoreLevels(database *sql.DB) error {
	categories := []string{
		"Работа на уроке",
		"Курсы по выбору",
		"Внеурочная активность",
		"Социальные поступки",
		"Дежурство",
	}
	for _, name := range categories {
		_, err := database.Exec(`INSERT OR IGNORE INTO categories (name, label) VALUES (?, ?);`, name, name)
		if err != nil {
			return err
		}
	}

	levels := []models.ScoreLevel{
		{Value: 100, Label: "Базовый", CategoryID: 1}, {Value: 200, Label: "Средний", CategoryID: 1}, {Value: 300, Label: "Высокий", CategoryID: 1},
		{Value: 100, Label: "Базовый", CategoryID: 2}, {Value: 200, Label: "Средний", CategoryID: 2}, {Value: 300, Label: "Высокий", CategoryID: 2},
		{Value: 100, Label: "Базовый", CategoryID: 3}, {Value: 200, Label: "Средний", CategoryID: 3}, {Value: 300, Label: "Высокий", CategoryID: 3},
		{Value: 100, Label: "Базовый", CategoryID: 4}, {Value: 200, Label: "Средний", CategoryID: 4}, {Value: 300, Label: "Высокий", CategoryID: 4},
		{Value: 100, Label: "Базовый", CategoryID: 5}, {Value: 200, Label: "Средний", CategoryID: 5}, {Value: 300, Label: "Высокий", CategoryID: 5},
	}
	for _, level := range levels {
		_, err := database.Exec(`
INSERT INTO score_levels (value, label, category_id)
VALUES (?, ?, ?)
ON CONFLICT(value, label, category_id) DO NOTHING
`, level.Value, level.Label, level.CategoryID)
		if err != nil {
			return fmt.Errorf("insert score_level: %w", err)
		}
	}
	// Добавление классов (1А - 11Д), если таблица пустая
	var count int
	err := database.QueryRow(`SELECT COUNT(*) FROM classes`).Scan(&count)
	if err != nil {
		return fmt.Errorf("ошибка при проверке таблицы classes: %w", err)
	}

	if count == 0 {
		classLetters := []string{"А", "Б", "В", "Г", "Д"}
		for grade := 1; grade <= 11; grade++ {
			for _, letter := range classLetters {
				className := fmt.Sprintf("%d%s", grade, letter)
				_, err := database.Exec(`
INSERT INTO classes (name)
VALUES (?)
ON CONFLICT(name) DO NOTHING;
`, className)
				if err != nil {
					return fmt.Errorf("insert class %s: %w", className, err)
				}
			}
		}
	}
	return nil
}
