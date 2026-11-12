package anvil

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	anvilSlog "github.com/tadel328/anvil/logger/slog"
)

func testLogger() Option {
	return WithLogger(anvilSlog.NewSlogLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
}

type fakeModule struct {
	name string

	mu    sync.Mutex
	calls []string

	startErr error
	stopErr  error
}

func (m *fakeModule) Name() string { return m.name }

func (m *fakeModule) Start(context.Context, *App) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "start:"+m.name)
	return m.startErr
}

func (m *fakeModule) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "stop:"+m.name)
	return m.stopErr
}

func (m *fakeModule) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		opts    []Option
		wantErr string
	}{
		{
			name:    "valid",
			appName: "api",
		},
		{
			name:    "empty app name",
			appName: "",
			wantErr: "app name is empty",
		},
		{
			name:    "nil module",
			appName: "api",
			opts:    []Option{With(nil)},
			wantErr: "module #0 is nil",
		},
		{
			name:    "duplicate module name",
			appName: "api",
			opts: []Option{With(
				&fakeModule{name: "db"},
				&fakeModule{name: "db"},
			)},
			wantErr: `module "db" is registered twice`,
		},
		{
			name:    "config error of a module",
			appName: "api",
			opts:    []Option{With(&badConfigModule{})},
			wantErr: "BAD is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append([]Option{testLogger()}, tt.opts...)
			app, err := New(tt.appName, opts...)

			if tt.wantErr != "" {
				if err != nil {
					t.Fatalf("New unexpected error: %v", err)
				}
				if app.Err() == nil {
					t.Fatalf("app.Err() = nil, want containing %q", tt.wantErr)
				}
				if !strings.Contains(app.Err().Error(), tt.wantErr) {
					t.Fatalf("app.Err() = %q, want containing %q", app.Err(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("New unexpected error: %v", err)
			}
			if app.Err() != nil {
				t.Fatalf("app.Err() = %v, want nil", app.Err())
			}
		})
	}
}

type badConfigModule struct{ fakeModule }

func (m *badConfigModule) Name() string                      { return "bad" }
func (m *badConfigModule) Start(context.Context, *App) error { return nil }
func (m *badConfigModule) Stop(context.Context) error        { return nil }
func (m *badConfigModule) Config() any {
	return &struct {
		Bad string `env:"BAD,required"`
	}{}
}

func TestStartOrder(t *testing.T) {
	rec := &recorder{}
	a := &recordedModule{name: "a", rec: rec}
	b := &recordedModule{name: "b", rec: rec}
	c := &recordedModule{name: "c", rec: rec}

	app, err := New("test", With(a, b, c), testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []string{"start:a", "start:b", "start:c", "stop:c", "stop:b", "stop:a"}

	combined := rec.snapshot()

	if len(combined) != len(want) {
		t.Fatalf("calls = %v, want %v", combined, want)
	}
	for i := range want {
		if combined[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, combined[i], want[i])
		}
	}
}

type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

type recordedModule struct {
	name string
	rec  *recorder
}

func (m *recordedModule) Name() string                      { return m.name }
func (m *recordedModule) Start(context.Context, *App) error { m.rec.add("start:" + m.name); return nil }
func (m *recordedModule) Stop(context.Context) error        { m.rec.add("stop:" + m.name); return nil }

func TestStartStopOrder(t *testing.T) {
	rec := &recorder{}
	app, err := New("test",
		With(&recordedModule{name: "a", rec: rec},
			&recordedModule{name: "b", rec: rec},
			&recordedModule{name: "c", rec: rec}),
		testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []string{"start:a", "start:b", "start:c", "stop:c", "stop:b", "stop:a"}
	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStartFailureRollsBack(t *testing.T) {
	rec := &recorder{}
	boom := errors.New("boom")
	app, err := New("test",
		With(&recordedModule{name: "a", rec: rec},
			&failingModule{name: "b", rec: rec, err: boom}),
		testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = app.Start(context.Background())
	if err == nil {
		t.Fatal("Start expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("Start error = %v, want wrapping %v", err, boom)
	}

	// b không được start; a đã start và phải được stop.
	want := []string{"start:a", "start:b", "stop:a"}
	got := rec.snapshot()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

type failingModule struct {
	name string
	rec  *recorder
	err  error
}

func (m *failingModule) Name() string { return m.name }
func (m *failingModule) Start(context.Context, *App) error {
	m.rec.add("start:" + m.name)
	return m.err
}
func (m *failingModule) Stop(context.Context) error {
	m.rec.add("stop:" + m.name)
	return nil
}

func TestShutdown(t *testing.T) {
	dbDown := errors.New("db down")
	tests := []struct {
		name    string
		cause   error
		wantErr error
	}{
		{name: "nil cause", cause: nil, wantErr: nil},
		{name: "with cause", cause: dbDown, wantErr: dbDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New("test", With(&fakeModule{name: "m"}), testLogger())
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := app.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}

			// Shutdown trước, rồi Wait — không cần signal.
			app.Shutdown(tt.cause)

			done := make(chan error, 1)
			go func() { done <- app.Wait(context.Background()) }()

			select {
			case err := <-done:
				if tt.wantErr == nil && err != nil {
					t.Fatalf("Wait = %v, want nil", err)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Fatalf("Wait = %v, want wrapping %v", err, tt.wantErr)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Wait did not return after Shutdown")
			}
		})
	}
}

func TestStartTwice(t *testing.T) {
	app, err := New("test", With(&fakeModule{name: "m"}), testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := app.Start(context.Background()); err == nil {
		t.Fatal("second Start expected error, got nil")
	}
}
