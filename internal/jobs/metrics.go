package jobs

import "github.com/prometheus/client_golang/prometheus"

var (
	jobRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "schoolbot_job_runs_total",
			Help: "Total background job runs",
		},
		[]string{"job"},
	)

	jobErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "schoolbot_job_errors_total",
			Help: "Total background job errors",
		},
		[]string{"job"},
	)

	jobDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "schoolbot_job_duration_seconds",
			Help:    "Background job duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"job"},
	)
)

func init() {
	prometheus.MustRegister(jobRuns, jobErrors, jobDuration)
}
