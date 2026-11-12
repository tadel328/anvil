package slog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/tadel328/anvil/logger"
)

func newTestLogger(level slog.Level) (logger.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level})
	return NewSlogLogger(slog.New(h)), &buf
}

func parseEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("parse log: %v (raw: %q)", err, buf.String())
	}
	return entry
}

func TestLevels(t *testing.T) {
	tests := []struct {
		name string
		emit func(logger.Logger)
		want string
	}{
		{"Debug", func(l logger.Logger) { l.Debug("m") }, "DEBUG"},
		{"Info", func(l logger.Logger) { l.Info("m") }, "INFO"},
		{"Warn", func(l logger.Logger) { l.Warn("m") }, "WARN"},
		{"Error", func(l logger.Logger) { l.Error("m") }, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, buf := newTestLogger(slog.LevelDebug)
			tt.emit(l)

			entry := parseEntry(t, buf)
			if entry["level"] != tt.want {
				t.Errorf("level = %v, want %v", entry["level"], tt.want)
			}
		})
	}
}

func TestLevelFiltering(t *testing.T) {
	tests := []struct {
		name    string
		handler slog.Level
		emit    func(logger.Logger)
		want    bool
	}{
		{"debug below info", slog.LevelInfo, func(l logger.Logger) { l.Debug("m") }, false},
		{"info at info", slog.LevelInfo, func(l logger.Logger) { l.Info("m") }, true},
		{"error above info", slog.LevelInfo, func(l logger.Logger) { l.Error("m") }, true},
		{"info below error", slog.LevelError, func(l logger.Logger) { l.Info("m") }, false},
		{"error at error", slog.LevelError, func(l logger.Logger) { l.Error("m") }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, buf := newTestLogger(tt.handler)
			tt.emit(l)

			if got := buf.Len() > 0; got != tt.want {
				t.Errorf("logged = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormattedMethods(t *testing.T) {
	tests := []struct {
		name    string
		emit    func(logger.Logger)
		wantMsg string
	}{
		{"Debugf", func(l logger.Logger) { l.Debugf("n=%d", 42) }, "n=42"},
		{"Infof", func(l logger.Logger) { l.Infof("hi %s", "go") }, "hi go"},
		{"Warnf", func(l logger.Logger) { l.Warnf("%v", true) }, "true"},
		{"Errorf", func(l logger.Logger) { l.Errorf("%d+%d", 1, 2) }, "1+2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, buf := newTestLogger(slog.LevelDebug)
			tt.emit(l)

			entry := parseEntry(t, buf)
			if entry["msg"] != tt.wantMsg {
				t.Errorf("msg = %q, want %q", entry["msg"], tt.wantMsg)
			}
		})
	}
}

func TestPrintMethodsJoinArgs(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want string
	}{
		{"single string", []any{"hi"}, "hi"},
		{"multiple strings no space", []any{"a", "b"}, "ab"},
		{"numbers get spaces", []any{1, 2, 3}, "1 2 3"},
		{"string then number", []any{"n=", 42}, "n=42"},
		{"number then string", []any{42, "!"}, "42!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, buf := newTestLogger(slog.LevelDebug)
			l.Info(tt.args...)

			entry := parseEntry(t, buf)
			if entry["msg"] != tt.want {
				t.Errorf("msg = %q, want %q", entry["msg"], tt.want)
			}
		})
	}
}

func TestWith(t *testing.T) {
	l, buf := newTestLogger(slog.LevelDebug)
	l.With("request_id", "abc").Info("m")

	entry := parseEntry(t, buf)
	if entry["request_id"] != "abc" {
		t.Errorf("request_id = %v, want abc", entry["request_id"])
	}
}

func TestWithDoesNotMutateOriginal(t *testing.T) {
	l, buf := newTestLogger(slog.LevelDebug)
	derived := l.With("k", "v")

	l.Info("first")
	first := parseEntry(t, buf)
	buf.Reset()

	derived.Info("second")
	second := parseEntry(t, buf)

	if _, ok := first["k"]; ok {
		t.Errorf("original logger leaked k: %v", first)
	}
	if second["k"] != "v" {
		t.Errorf("derived k = %v, want v", second["k"])
	}
}

func TestWithFields(t *testing.T) {
	l, buf := newTestLogger(slog.LevelDebug)
	l.WithFields(logger.Fields{"a": 1, "b": "two"}).Info("m")

	entry := parseEntry(t, buf)
	if entry["a"] != float64(1) {
		t.Errorf("a = %v, want 1", entry["a"])
	}
	if entry["b"] != "two" {
		t.Errorf("b = %v, want two", entry["b"])
	}
}

func TestWithFieldsEmpty(t *testing.T) {
	l, buf := newTestLogger(slog.LevelDebug)
	l.WithFields(logger.Fields{}).Info("m")

	entry := parseEntry(t, buf)
	if entry["msg"] != "m" {
		t.Errorf("msg = %v, want m", entry["msg"])
	}
}

func TestWithFieldsSortsKeys(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := NewSlogLogger(slog.New(h))

	l.WithFields(logger.Fields{"zebra": 1, "apple": 2, "mango": 3}).Info("m")

	out := buf.String()
	iA := strings.Index(out, "apple=")
	iM := strings.Index(out, "mango=")
	iZ := strings.Index(out, "zebra=")

	if iA < 0 || iM < 0 || iZ < 0 {
		t.Fatalf("missing key in output %q (apple=%d mango=%d zebra=%d)", out, iA, iM, iZ)
	}
	if !(iA < iM && iM < iZ) {
		t.Errorf("keys not sorted: apple=%d mango=%d zebra=%d in %q", iA, iM, iZ, out)
	}
}

func TestChaining(t *testing.T) {
	l, buf := newTestLogger(slog.LevelDebug)
	l.With("service", "api").
		WithFields(logger.Fields{"env": "prod"}).
		With("req", 1).
		Info("m")

	entry := parseEntry(t, buf)
	if entry["service"] != "api" {
		t.Errorf("service = %v, want api", entry["service"])
	}
	if entry["env"] != "prod" {
		t.Errorf("env = %v, want prod", entry["env"])
	}
	if entry["req"] != float64(1) {
		t.Errorf("req = %v, want 1", entry["req"])
	}
}
