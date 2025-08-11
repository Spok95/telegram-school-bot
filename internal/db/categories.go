package db

import (
	"database/sql"
	"errors"

	"github.com/Spok95/telegram-school-bot/internal/models"
)

func GetLevelByID(database *sql.DB, levelID int) (*models.ScoreLevel, error) {
	var level models.ScoreLevel
	err := database.QueryRow("SELECT id, value, label, category_id FROM score_levels WHERE id = ?", levelID).Scan(&level.ID, &level.Value, &level.Label, &level.CategoryID)
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

// Категории
// список (includeInactive=true — вернём и скрытые)
func GetCategories(database *sql.DB, includeInactive bool) ([]models.Category, error) {
	query := "SELECT id, name, label, is_active FROM categories"
	if !includeInactive {
		query += " WHERE is_active = 1"
	}
	query += " ORDER BY id"

	rows, err := database.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Label, &c.IsActive); err != nil {
			return nil, err
		}
		out = append(out, c)
	}

	return out, nil
}

// по id
func GetCategoryByID(database *sql.DB, id int64) (*models.Category, error) {
	var c models.Category
	err := database.QueryRow(
		"SELECT id, name, label, is_active FROM categories WHERE id = ?",
		id,
	).Scan(&c.ID, &c.Name, &c.Label, &c.IsActive)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// создать (name, label) — label уже есть в схеме
func CreateCategory(database *sql.DB, name, label string) (int64, error) {
	res, err := database.Exec(
		"INSERT INTO categories(name, label, is_active) VALUES(?,?,1)",
		name, label,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// переименовать (меняем name; при желании добавь и UpdateCategoryLabel)
func RenameCategory(database *sql.DB, id int64, name string) error {
	res, err := database.Exec(
		"UPDATE categories SET name = ? WHERE id = ?",
		name, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("category not found")
	}
	return nil
}

// включить/выключить (is_active)
func SetCategoryActive(database *sql.DB, id int64, active bool) error {
	val := 0
	if active {
		val = 1
	}
	res, err := database.Exec(
		"UPDATE categories SET is_active = ? WHERE id = ?",
		val, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("category not found")
	}
	return nil
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

// Уровни
// список уровней категории (includeInactive как выше)
func GetLevelsByCategoryIDFull(database *sql.DB, catID int64, includeInactive bool) ([]models.ScoreLevel, error) {
	query := "SELECT id, value, label, category_id, is_active FROM score_levels WHERE category_id = ?"
	if !includeInactive {
		query += " AND is_active = 1"
	}
	query += " ORDER BY value"

	rows, err := database.Query(query, catID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.ScoreLevel
	for rows.Next() {
		var l models.ScoreLevel
		if err := rows.Scan(&l.ID, &l.Value, &l.Label, &l.CategoryID, &l.IsActive); err != nil {
			return nil, err
		}
		out = append(out, l)
	}

	return out, nil
}

func CreateLevel(database *sql.DB, catID int64, value int, label string) (int64, error) {
	res, err := database.Exec(
		"INSERT INTO score_levels(value, label, category_id, is_active) VALUES(?,?,?,1)",
		value, label, catID,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func RenameLevel(database *sql.DB, id int64, label string) error {
	res, err := database.Exec(
		"UPDATE score_levels SET label = ? WHERE id = ?",
		label, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("level not found")
	}
	return nil
}

func SetLevelActive(database *sql.DB, id int64, active bool) error {
	val := 0
	if active {
		val = 1
	}
	res, err := database.Exec(
		"UPDATE score_levels SET is_active = ? WHERE id = ?",
		val, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("level not found")
	}
	return nil
}
