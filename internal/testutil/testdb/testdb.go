//go:build testutil
// +build testutil

package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type DBHandle struct {
	DB     *sql.DB
	cancel func()
	stop   func(context.Context) error
}

func (h *DBHandle) Close() {
	if h.DB != nil {
		_ = h.DB.Close()
	}
	if h.stop != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.stop(ctx)
	}
	if h.cancel != nil {
		h.cancel()
	}
}

func Start(ctx context.Context) (*DBHandle, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)

	pg, err := postgres.RunContainer(ctx,
		tc.WithImage("postgres:17-alpine"),
		postgres.WithDatabase("school"),
		postgres.WithUsername("school"),
		postgres.WithPassword("school"),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	uri, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pg.Terminate(ctx)
		cancel()
		return nil, err
	}

	db, err := sql.Open("postgres", uri)
	if err != nil {
		_ = pg.Terminate(ctx)
		cancel()
		return nil, err
	}
	if err := waitReady(ctx, db); err != nil {
		_ = pg.Terminate(ctx)
		cancel()
		return nil, err
	}

	if err := applyMigrations(ctx, db); err != nil {
		_ = pg.Terminate(ctx)
		cancel()
		return nil, err
	}

	return &DBHandle{
		DB:     db,
		cancel: cancel,
		stop:   pg.Terminate,
	}, nil
}

func waitReady(ctx context.Context, db *sql.DB) error {
	dead := time.Now().Add(20 * time.Second)
	for time.Now().Before(dead) {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("db not ready")
}

func repoRoot() (string, error) {
	wd, _ := os.Getwd()
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("go.mod not found from %s", wd)
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	migDir := filepath.Join(root, "internal", "bot", "handlers", "migrations")
	ents, err := os.ReadDir(migDir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, filepath.Join(migDir, e.Name()))
		}
	}
	sort.Strings(files) // ← критично
	fmt.Println("[migrate] applying:")
	for _, f := range files {
		fmt.Println(" -", filepath.Base(f))
	}
	for _, f := range files {
		sqlText, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		up := extractGooseUp(string(sqlText))
		if strings.TrimSpace(up) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, up); err != nil {
			return fmt.Errorf("migration %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

// Берём только блок между "-- +goose Up" и "-- +goose Down"
func extractGooseUp(s string) string {
	// нормализуем переносы
	text := s
	upTag := "-- +goose Up"
	downTag := "-- +goose Down"
	upIdx := strings.Index(text, upTag)
	if upIdx == -1 {
		// нет маркеров — исполняем всё как есть
		return text
	}
	rest := text[upIdx+len(upTag):]
	downIdx := strings.Index(rest, downTag)
	if downIdx == -1 {
		return rest
	}
	return rest[:downIdx]
}
