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
	return nil
}
