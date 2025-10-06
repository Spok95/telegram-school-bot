package logging

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Log struct {
	Base   *zap.Logger
	Sugar  *zap.SugaredLogger
	Level  zap.AtomicLevel
	Closer func()
}

func Init(level, env string) (*Log, error) {
	lvl := zap.NewAtomicLevel()
	if err := lvl.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		lvl = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	var cfg zap.Config
	if strings.ToLower(env) == "prod" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}
	cfg.Level = lvl
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	base, err := cfg.Build(zap.AddStacktrace(zap.ErrorLevel))
	if err != nil {
		return nil, err
	}
	return &Log{
		Base:   base,
		Sugar:  base.Sugar(),
		Level:  lvl,
		Closer: func() { _ = base.Sync() },
	}, nil
}
