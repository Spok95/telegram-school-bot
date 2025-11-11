package observability

import (
	"log"
	"time"

	"github.com/Spok95/telegram-school-bot/internal/logging"
	"github.com/getsentry/sentry-go"
)

var (
	curEnv string
	lg     *logging.Log
)

func SetLogger(l *logging.Log) { lg = l }

func InitSentry(dsn, env, release string) (func(), error) {
	curEnv = env
	if dsn == "" {
		return func() {}, nil
	}
	opts := sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      env,
		Release:          release,
		Debug:            env != "prod",
		TracesSampleRate: 0.2,
	}
	if err := sentry.Init(opts); err != nil {
		return func() {}, err
	}
	return func() { sentry.Flush(2 * time.Second) }, nil
}

func CaptureErr(err error) {
	if err == nil {
		return
	}
	if lg != nil {
		lg.Sugar.Errorw("captured error", "err", err)
	} else {
		log.Printf("captured error: %v", err)
	}
	if curEnv != "prod" {
		if id := sentry.CaptureException(err); id != nil {
			idStr := string(*id)
			if lg != nil {
				lg.Sugar.Infow("sentry event captured", "event_id", idStr)
			} else {
				log.Printf("sentry event captured event_id=%s", idStr)
			}
		}
		return
	}
	sentry.CaptureException(err)
}
