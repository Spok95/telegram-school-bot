package db

import (
	"database/sql"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

func GetCategoryByID(database *sql.DB, id int) (string, error) {
	var name string
	err := database.QueryRow(`SELECT name FROM categories WHERE id=?`, id).Scan(&name)
	return name, err
}

func GetLevelsByCategoryID(database *sql.DB, categoryID int) ([]models.ScoreLevel, error) {
	rows, err := database.Query("SELECT id, value, label, category_id FROM score_levels WHERE category_id = ?", categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var levels []models.ScoreLevel
	for rows.Next() {
		var level models.ScoreLevel
		if err := rows.Scan(&level.ID, &level.Value, &level.Label, &level.CategoryID); err != nil {
			return nil, err
		}
		levels = append(levels, level)
	}
	return levels, nil
}

func GetLevelByID(database *sql.DB, levelID int) (*models.ScoreLevel, error) {
	var level models.ScoreLevel
	err := database.QueryRow("SELECT id, value, label, category_id FROM score_levels WHERE id = $1", levelID).Scan(&level.ID, &level.Value, &level.Label, &level.CategoryID)
	if err != nil {
		return nil, err
	}
	return &level, nil
}

// Получение всех категорий
func GetAllCategories(database *sql.DB, role string) ([]models.Category, error) {
	var rows *sql.Rows
	var err error

	if role == "admin" || role == "administration" {
		rows, err = database.Query("SELECT id, name, label FROM categories ORDER BY id")
	} else {
		rows, err = database.Query("SELECT id, name, label FROM categories WHERE name != 'Аукцион' ORDER BY id")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Label); err != nil {
			return nil, err
		}
		categories = append(categories, c)
	}
	return categories, nil
}
