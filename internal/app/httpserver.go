package app

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/metrics"
)

type HTTPServer struct {
	srv *http.Server
}

func StartHTTP(ctx context.Context, addr string, db *sql.DB) *HTTPServer {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
		defer cancel()
		t0 := time.Now()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "db not ok: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		metrics.ObserveDBPing(time.Since(t0))
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("/metrics", metrics.Handler())

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		_ = srv.ListenAndServe() // закрываем аккуратно при Shutdown
	}()

	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()

	return &HTTPServer{srv: srv}
}
