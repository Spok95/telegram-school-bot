package observability

import (
	"time"

	"github.com/getsentry/sentry-go"
)

func InitSentry(dsn, env, release string) (func(), error) {
	if dsn == "" {
		return func() {}, nil
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: env,
		Release:     release,
	}); err != nil {
		return func() {}, err
	}
	return func() { sentry.Flush(2 * time.Second) }, nil
}

func CaptureErr(err error) {
	if err != nil {
		sentry.CaptureException(err)
	}
}
