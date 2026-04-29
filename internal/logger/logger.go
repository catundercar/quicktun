// Package logger constructs a zap.Logger using the application's LogConfig.
//
// When LogConfig.Path is set, output is rotated by lumberjack. When empty,
// logs go to stdout (useful in tests and dev).
package logger

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/tulip/quicktun/internal/config"
)

// String wraps zap.String so callers don't import zap directly. Add more
// helpers (Int, Error, Duration, ...) as the surface grows.
func String(k, v string) zap.Field { return zap.String(k, v) }

// New builds a production zap.Logger from the given LogConfig.
func New(cfg config.LogConfig) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("logger: parse level %q: %w", cfg.Level, err)
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	enc := zapcore.NewJSONEncoder(encCfg)

	var sink zapcore.WriteSyncer
	if cfg.Path == "" {
		sink = zapcore.AddSync(os.Stdout)
	} else {
		sink = zapcore.AddSync(&lumberjack.Logger{
			Filename:   cfg.Path,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   true,
		})
	}

	core := zapcore.NewCore(enc, sink, level)
	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), nil
}
