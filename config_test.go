package anvil

import (
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		check   func(t *testing.T, cfg *testConfig)
	}{
		{
			name: "string",
			env:  map[string]string{"NAME": "anvil"},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Name != "anvil" {
					t.Errorf("Name = %q, want anvil", cfg.Name)
				}
			},
		},
		{
			name: "bool",
			env:  map[string]string{"DEBUG": "true"},
			check: func(t *testing.T, cfg *testConfig) {
				if !cfg.Debug {
					t.Error("Debug = false, want true")
				}
			},
		},
		{
			name: "int",
			env:  map[string]string{"PORT": "8080"},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Port != 8080 {
					t.Errorf("Port = %d, want 8080", cfg.Port)
				}
			},
		},
		{
			name: "duration",
			env:  map[string]string{"TIMEOUT": "5s"},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Timeout != 5*time.Second {
					t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
				}
			},
		},
		{
			name: "slice with trim",
			env:  map[string]string{"TAGS": "a, b , c"},
			check: func(t *testing.T, cfg *testConfig) {
				want := []string{"a", "b", "c"}
				if len(cfg.Tags) != len(want) {
					t.Fatalf("Tags = %v, want %v", cfg.Tags, want)
				}
				for i := range want {
					if cfg.Tags[i] != want[i] {
						t.Errorf("Tags[%d] = %q, want %q", i, cfg.Tags[i], want[i])
					}
				}
			},
		},
		{
			name: "pointer",
			env:  map[string]string{"RATIO": "0.5"},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Ratio == nil || *cfg.Ratio != 0.5 {
					t.Errorf("Ratio = %v, want 0.5", cfg.Ratio)
				}
			},
		},
		{
			name: "default when unset",
			env:  map[string]string{},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Env != "dev" {
					t.Errorf("Env = %q, want default dev", cfg.Env)
				}
			},
		},
		{
			name: "default when empty",
			env:  map[string]string{"ENV": ""},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Env != "dev" {
					t.Errorf("Env = %q, want default dev", cfg.Env)
				}
			},
		},
		{
			name: "explicit overrides default",
			env:  map[string]string{"ENV": "prod"},
			check: func(t *testing.T, cfg *testConfig) {
				if cfg.Env != "prod" {
					t.Errorf("Env = %q, want prod", cfg.Env)
				}
			},
		},
		{
			name:    "invalid int",
			env:     map[string]string{"PORT": "abc"},
			wantErr: "PORT:",
		},
		{
			name:    "secret error hides value",
			env:     map[string]string{"SECRET": "not-a-number"},
			wantErr: "SECRET: invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg testConfig
			err := LoadConfig(&cfg, "", lookupFrom(tt.env))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadConfig expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadConfig error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadConfig unexpected error: %v", err)
			}
			tt.check(t, &cfg)
		})
	}
}

type testConfig struct {
	Name    string        `env:"NAME"`
	Debug   bool          `env:"DEBUG"`
	Port    int           `env:"PORT"`
	Timeout time.Duration `env:"TIMEOUT"`
	Tags    []string      `env:"TAGS"`
	Ratio   *float64      `env:"RATIO"`
	Env     string        `env:"ENV" default:"dev"`
	Secret  int           `env:"SECRET,secret"`
}

func TestLoadConfigSecretHidesValue(t *testing.T) {
	var cfg struct {
		Secret int `env:"SECRET,secret"`
	}
	err := LoadConfig(&cfg, "", lookupFrom(map[string]string{"SECRET": "super-secret-123"}))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "super-secret-123") {
		t.Fatalf("secret value leaked in error: %v", err)
	}
}

func TestLoadConfigRequired(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "missing",
			env:     nil,
			wantErr: "VAL is required",
		},
		{
			name: "provided",
			env:  map[string]string{"VAL": "x"},
		},
		{
			name:    "empty string counts as missing",
			env:     map[string]string{"VAL": ""},
			wantErr: "VAL is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg struct {
				Val string `env:"VAL,required"`
			}
			err := LoadConfig(&cfg, "", lookupFrom(tt.env))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Val != "x" {
				t.Errorf("Val = %q, want x", cfg.Val)
			}
		})
	}
}

func TestLoadConfigSecretDoesNotLeakValue(t *testing.T) {
	var cfg struct {
		Key string `env:"KEY,secret"`
	}
	const raw = "super-secret-value-123"

	err := LoadConfig(&cfg, "", lookupFrom(map[string]string{"KEY": raw}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Key != raw {
		t.Errorf("Key = %q, want %q", cfg.Key, raw)
	}
	var cfg2 struct {
		Key int `env:"KEY,secret"`
	}
	err = LoadConfig(&cfg2, "", lookupFrom(map[string]string{"KEY": "abc"}))
	if err == nil {
		t.Fatal("expected parse error for non-numeric secret")
	}
	if strings.Contains(err.Error(), "abc") {
		t.Fatalf("secret value leaked in error: %v", err)
	}
}
