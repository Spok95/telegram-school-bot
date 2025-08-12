package db

import (
	"database/sql"
	"fmt"
	"log"

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
	_, err := database.Exec(`INSERT OR IGNORE INTO categories (name, label) VALUES (?, ?)`, "Аукцион", "Аукцион")
	if err != nil {
		log.Fatalf("ошибка вставки категории Аукцион: %v", err)
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
ON CONFLICT(category_id, value) DO NOTHING
`, level.Value, level.Label, level.CategoryID)
		if err != nil {
			return fmt.Errorf("insert score_level: %w", err)
		}
	}
	// Добавление классов (1А - 11Д), если таблица пустая
	var count int
	err = database.QueryRow(`SELECT COUNT(*) FROM classes`).Scan(&count)
	if err != nil {
		return fmt.Errorf("ошибка при проверке таблицы classes: %w", err)
	}

	if count == 0 {
		classLetters := []string{"А", "Б", "В", "Г", "Д"}
		for grade := 1; grade <= 11; grade++ {
			for _, letter := range classLetters {
				_, err := database.Exec(`
INSERT INTO classes (number, letter)
VALUES (?, ?)
ON CONFLICT(number, letter) DO NOTHING;
`, grade, letter)

				if err != nil {
					return fmt.Errorf("insert class %d%s: %w", grade, letter, err)
				}
			}
		}
	}
	return nil
}

func SeedStudents(database *sql.DB) error {
	log.Println("🧪 Наполнение таблицы users тестовыми учениками...")

	startTelegramID := int64(1000000001)
	classLetters := []string{"А", "Б", "В", "Г", "Д"}

	for grade := 1; grade <= 11; grade++ {
		for _, letter := range classLetters {
			var classID int
			err := database.QueryRow(`
			SELECT id FROM classes WHERE number = ? AND letter = ? LIMIT 1
		`, grade, letter).Scan(&classID)
			if err != nil {
				return fmt.Errorf("❌ не удалось найти class_id для %d%s: %w", grade, letter, err)
			}

			for i := 1; i <= 10; i++ {
				name := fmt.Sprintf("Ученик %d%s %d", grade, letter, i)
				telegramID := startTelegramID
				startTelegramID++

				_, err := database.Exec(`
INSERT OR IGNORE INTO users (telegram_id, name, role, class_number, class_letter, class_id, confirmed, is_active)
VALUES (?, ?, 'student', ?, ?, ?, 1, 1);
`, telegramID, name, grade, letter, classID)
				if err != nil {
					return fmt.Errorf("❌ ошибка при вставке ученика %s: %w", name, err)
				}
			}
		}
	}

	log.Println("✅ Ученики успешно добавлены.")
	return nil
}
