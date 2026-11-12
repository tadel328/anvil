package slog

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/tadel328/anvil/logger"
)

type slogLogger struct {
	l *slog.Logger
}

func NewSlogLogger(l *slog.Logger) logger.Logger {
	return &slogLogger{l: l}
}

func (s slogLogger) print(level slog.Level, args []any) {
	if s.l.Enabled(context.Background(), level) {
		s.l.Log(context.Background(), level, fmt.Sprint(args...))
	}
}

func (s slogLogger) printf(level slog.Level, format string, args []any) {
	if s.l.Enabled(context.Background(), level) {
		s.l.Log(context.Background(), level, fmt.Sprintf(format, args...))
	}
}

func (s slogLogger) Debug(args ...any) { s.print(slog.LevelDebug, args) }
func (s slogLogger) Info(args ...any)  { s.print(slog.LevelInfo, args) }
func (s slogLogger) Warn(args ...any)  { s.print(slog.LevelWarn, args) }
func (s slogLogger) Error(args ...any) { s.print(slog.LevelError, args) }

func (s slogLogger) Debugf(format string, args ...any) { s.printf(slog.LevelDebug, format, args) }
func (s slogLogger) Infof(format string, args ...any)  { s.printf(slog.LevelInfo, format, args) }
func (s slogLogger) Warnf(format string, args ...any)  { s.printf(slog.LevelWarn, format, args) }
func (s slogLogger) Errorf(format string, args ...any) { s.printf(slog.LevelError, format, args) }

func (s slogLogger) With(key string, value any) logger.Logger {
	return slogLogger{l: s.l.With(slog.Any(key, value))}
}

func (s slogLogger) WithFields(fields logger.Fields) logger.Logger {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	// sort unordered map
	sort.Strings(keys)

	attrs := make([]any, 0, len(keys))
	for _, key := range keys {
		attrs = append(attrs, slog.Any(key, fields[key]))
	}

	return slogLogger{l: s.l.With(attrs...)}
}
