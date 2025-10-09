package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	BotUpdates = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "schoolbot", Name: "updates_total", Help: "Processed telegram updates",
	})
	HandlerErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "schoolbot", Name: "handler_errors_total", Help: "Handler errors",
	})
	DBPing = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "schoolbot", Name: "db_ping_seconds", Help: "DB ping latency",
		Buckets: prometheus.DefBuckets,
	})
)

var (
	UpdateDropsRateLimit = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "tg_updates_dropped_ratelimit_total",
			Help: "Updates dropped by per-chat rate limiter",
		},
	)
	UpdateDropsDedup = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "tg_updates_dropped_dedup_total",
			Help: "Updates dropped by dedup guard",
		},
	)
)

func init() {
	prometheus.MustRegister(BotUpdates, HandlerErrors, DBPing)
}

func Handler() http.Handler { return promhttp.Handler() }

func ObserveDBPing(d time.Duration) { DBPing.Observe(d.Seconds()) }
