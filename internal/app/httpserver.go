package app

import (
	"context"
	"database/sql"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
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

	// readiness: короткий ping БД с таймаутом
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 700*time.Millisecond)
		defer cancel()

		start := time.Now()
		if err := db.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db not ready"))
			return
		}
		metrics.ObserveDBPing(time.Since(start))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/export/consultations.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		q := r.URL.Query()
		parseDate := func(key string, def time.Time) time.Time {
			s := strings.TrimSpace(q.Get(key))
			if s == "" {
				return def
			}
			// допускаем YYYY-MM-DD и RFC3339
			if t, err := time.Parse("2006-01-02", s); err == nil {
				return t
			}
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
			return def
		}
		now := time.Now()
		from := parseDate("from", now.AddDate(0, 0, -7))
		to := parseDate("to", now.AddDate(0, 1, 0))

		var teacherID *int64
		if s := strings.TrimSpace(q.Get("teacher_id")); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				teacherID = &v
			}
		}
		var classID *int64
		if s := strings.TrimSpace(q.Get("class_id")); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				classID = &v
			}
		}

		rows, err := db.QueryContext(r.Context(), `
        SELECT s.start_at, s.end_at,
               t.name as teacher_name,
               c.name as class_name,
               CASE WHEN s.booked_by_id IS NULL THEN 'free' ELSE 'booked' END as status,
               p.name as parent_name
        FROM consult_slots s
        LEFT JOIN users t ON t.id = s.teacher_id
        LEFT JOIN users p ON p.id = s.booked_by_id
        LEFT JOIN classes c ON c.id = s.class_id
        WHERE s.start_at >= $1 AND s.start_at < $2
          AND ($3::bigint IS NULL OR s.teacher_id = $3)
          AND ($4::bigint IS NULL OR s.class_id = $4)
        ORDER BY s.start_at
    `, from.UTC(), to.UTC(), teacherID, classID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("query error"))
			return
		}
		defer func() { _ = rows.Close() }()

		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"start", "end", "teacher", "class", "status", "parent"})
		loc := time.Local
		for rows.Next() {
			var start, end time.Time
			var teacher, className, status, parent sql.NullString
			if err := rows.Scan(&start, &end, &teacher, &className, &status, &parent); err != nil {
				continue
			}
			rec := []string{
				start.In(loc).Format(time.RFC3339),
				end.In(loc).Format(time.RFC3339),
				teacher.String,
				className.String,
				status.String,
				parent.String,
			}
			_ = cw.Write(rec)
		}
		cw.Flush()
	})

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
