package db

import (
	"context"
	"database/sql"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func MustOpen() (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		panic(err)
	}
	// Клиентский таймаут на первичную проверку соединения
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			panic(err)
		}
	}

	// Пределы пула
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(10 * time.Minute)

	// Если по каким-то причинам не сработают настройки роли — подстраховка.
	// Эти SET применяются к конкретной сессии, так что их влияние ограничено,
	// а роль по initdb всё равно задаёт дефолты глобально.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx, `
			SET statement_timeout = '30s';
			SET lock_timeout = '5s';
			SET idle_in_transaction_session_timeout = '30s';
			SET TIME ZONE 'Europe/Moscow';
		`)
	}
	return db, nil
}
