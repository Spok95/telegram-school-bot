package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Spok95/telegram-school-bot/internal/ctxutil"
	"github.com/Spok95/telegram-school-bot/internal/models"
)

func GetLevelByID(ctx context.Context, database *sql.DB, levelID int) (*models.ScoreLevel, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var level models.ScoreLevel
	err := database.QueryRowContext(ctx, "SELECT id, value, label, category_id FROM score_levels WHERE id = $1", levelID).Scan(&level.ID, &level.Value, &level.Label, &level.CategoryID)
	if err != nil {
		return nil, err
	}
	return &level, nil
}

// GetCategories список (includeInactive=true — вернём и скрытые)
func GetCategories(ctx context.Context, database *sql.DB, includeInactive bool) ([]models.Category, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	query := "SELECT id, name, label, is_active FROM categories"
	if !includeInactive {
		query += " WHERE is_active = TRUE"
	}
	query += " ORDER BY id"

	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

// GetCategoryByID по id
func GetCategoryByID(ctx context.Context, database *sql.DB, id int64) (*models.Category, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var c models.Category
	err := database.QueryRowContext(ctx,
		"SELECT id, name, label, is_active FROM categories WHERE id = $1",
		id,
	).Scan(&c.ID, &c.Name, &c.Label, &c.IsActive)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCategory создать (name, label) — label уже есть в схеме
func CreateCategory(ctx context.Context, database *sql.DB, name, label string) (int64, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var id int64
	err := database.
		QueryRowContext(ctx,
			"INSERT INTO categories(name, label, is_active) VALUES($1,$2,TRUE) RETURNING id",
			name, label,
		).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// RenameCategory переименовать (меняем name; при желании добавь и UpdateCategoryLabel)
func RenameCategory(ctx context.Context, database *sql.DB, id int64, name string) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	res, err := database.ExecContext(ctx,
		"UPDATE categories SET name = $1 WHERE id = $2",
		name, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("категория не найдена")
	}
	return nil
}

// SetCategoryActive включить/выключить (is_active)
func SetCategoryActive(ctx context.Context, database *sql.DB, id int64, active bool) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	res, err := database.ExecContext(ctx,
		"UPDATE categories SET is_active = $1 WHERE id = $2",
		active, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("категория не найдена")
	}
	return nil
}

// GetLevelsByCategoryIDFull список уровней категории (includeInactive как выше)
func GetLevelsByCategoryIDFull(ctx context.Context, database *sql.DB, catID int64, includeInactive bool) ([]models.ScoreLevel, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	query := "SELECT id, value, label, category_id, is_active FROM score_levels WHERE category_id = $1"
	if !includeInactive {
		query += " AND is_active = TRUE"
	}
	query += " ORDER BY value"

	rows, err := database.QueryContext(ctx, query, catID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

func CreateLevel(ctx context.Context, database *sql.DB, catID int64, value int, label string) (int64, error) {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var id int64
	err := database.QueryRowContext(ctx,
		"INSERT INTO score_levels(value, label, category_id, is_active) VALUES($1,$2,$3,TRUE) RETURNING id",
		value, label, catID,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func RenameLevel(ctx context.Context, database *sql.DB, id int64, label string) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	res, err := database.ExecContext(ctx,
		"UPDATE score_levels SET label = $1 WHERE id = $2",
		label, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("уровень не найден")
	}
	return nil
}

func SetLevelActive(ctx context.Context, database *sql.DB, id int64, active bool) error {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	res, err := database.ExecContext(ctx,
		"UPDATE score_levels SET is_active = $1 WHERE id = $2",
		active, id,
	)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return errors.New("уровень не найден")
	}
	return nil
}

func GetCategoryIDByName(ctx context.Context, database *sql.DB, name string) int {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var id int
	row := database.QueryRowContext(ctx, `SELECT id FROM categories WHERE name = $1`, name)
	_ = row.Scan(&id)
	return id
}

func GetCategoryNameByID(ctx context.Context, database *sql.DB, id int) string {
	ctx, cancel := ctxutil.WithDBTimeout(ctx)
	defer cancel()
	var name string
	row := database.QueryRowContext(ctx, `SELECT name FROM categories WHERE id = $1`, id)
	_ = row.Scan(&name)
	return name
}
