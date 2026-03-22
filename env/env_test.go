package env_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/victorialuquet/nimbus/env"
	"github.com/victorialuquet/nimbus/internal/envparse"
)

// fakeLookuper builds an envparse.Lookuper from a static map.
func fakeLookuper(kv map[string]string) envparse.Lookuper {
	return func(key string) (string, bool) {
		v, ok := kv[key]
		return v, ok
	}
}

// ── Setup ──────────────────────────────────────────────────────────────────

type appConfig struct {
	Host string `env:"HOST,required"`
	Port int    `env:"PORT,default=8080" validate:"min=1,max=65535"`
}

func TestSetup_happy_path(t *testing.T) {
	cfg, err := env.Setup[appConfig](context.Background(),
		env.WithLookuper(fakeLookuper(map[string]string{
			"HOST": "localhost",
			"PORT": "9000",
		})),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != 9000 {
		t.Errorf("Port = %d, want 9000", cfg.Port)
	}
}

func TestSetup_default_port(t *testing.T) {
	cfg, err := env.Setup[appConfig](context.Background(),
		env.WithLookuper(fakeLookuper(map[string]string{
			"HOST": "example.com",
		})),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080 (default)", cfg.Port)
	}
}

func TestSetup_missing_required(t *testing.T) {
	_, err := env.Setup[appConfig](context.Background(),
		env.WithLookuper(fakeLookuper(nil)),
	)
	if err == nil {
		t.Error("expected error for missing required field, got nil")
	}
}

func TestSetup_validation_error(t *testing.T) {
	// PORT=0 fails the validate:"min=1" rule.
	_, err := env.Setup[appConfig](context.Background(),
		env.WithLookuper(fakeLookuper(map[string]string{
			"HOST": "localhost",
			"PORT": "0",
		})),
	)
	if err == nil {
		t.Error("expected validation error for port=0, got nil")
	}
}

func TestSetup_returns_pointer(t *testing.T) {
	cfg, err := env.Setup[appConfig](context.Background(),
		env.WithLookuper(fakeLookuper(map[string]string{
			"HOST": "h",
		})),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Error("Setup returned nil pointer")
	}
}

// ── Inject / From / MustFrom ───────────────────────────────────────────────

type dbConfig struct {
	URL string
}

func TestInjectFrom_roundtrip(t *testing.T) {
	original := &dbConfig{URL: "postgres://localhost/test"}
	ctx := env.Inject[dbConfig](context.Background(), original)

	got, err := env.From[dbConfig](ctx)
	if err != nil {
		t.Fatalf("From returned error: %v", err)
	}
	if got.URL != original.URL {
		t.Errorf("URL = %q, want %q", got.URL, original.URL)
	}
}

func TestFrom_not_found(t *testing.T) {
	_, err := env.From[dbConfig](context.Background())
	if err == nil {
		t.Error("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, env.ErrNotFound) {
		t.Errorf("error = %v, want wrapping ErrNotFound", err)
	}
}

func TestMustFrom_returns_value(t *testing.T) {
	cfg := &dbConfig{URL: "postgres://prod/db"}
	ctx := env.Inject[dbConfig](context.Background(), cfg)

	got := env.MustFrom[dbConfig](ctx)
	if got.URL != cfg.URL {
		t.Errorf("URL = %q, want %q", got.URL, cfg.URL)
	}
}

func TestMustFrom_panics_when_missing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustFrom to panic when config is missing")
		}
	}()
	_ = env.MustFrom[dbConfig](context.Background())
}

// ── Different types do not collide ─────────────────────────────────────────

type cacheConfig struct {
	Addr string
}

func TestInject_different_types_no_collision(t *testing.T) {
	db := &dbConfig{URL: "db://host"}
	cache := &cacheConfig{Addr: "cache:6379"}

	ctx := context.Background()
	ctx = env.Inject[dbConfig](ctx, db)
	ctx = env.Inject[cacheConfig](ctx, cache)

	gotDB, err := env.From[dbConfig](ctx)
	if err != nil || gotDB.URL != db.URL {
		t.Errorf("dbConfig: err=%v URL=%q", err, gotDB.URL)
	}

	gotCache, err := env.From[cacheConfig](ctx)
	if err != nil || gotCache.Addr != cache.Addr {
		t.Errorf("cacheConfig: err=%v Addr=%q", err, gotCache.Addr)
	}
}

// ── WithDotenv ─────────────────────────────────────────────────────────────

func TestSetup_withDotenv(t *testing.T) {
	// Write a temp .env file and change to that directory so Setup can find it.
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte("HOST=from-dotenv\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// godotenv.Overload uses os.Setenv so we must restore afterwards.
	orig, hadOrig := os.LookupEnv("HOST")
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("HOST", orig)
		} else {
			os.Unsetenv("HOST")
		}
	})

	cfg, err := env.Setup[appConfig](context.Background(),
		env.WithDotenv(dotenv),
	)
	if err != nil {
		t.Fatalf("Setup error: %v", err)
	}
	if cfg.Host != "from-dotenv" {
		t.Errorf("Host = %q, want from-dotenv", cfg.Host)
	}
}

func TestSetup_withDotenv_missing_file(t *testing.T) {
	// A missing .env file should be silently ignored (consistent with other
	// dotenv implementations). Setup should still succeed if required fields
	// are provided via WithLookuper.
	_, err := env.Setup[appConfig](context.Background(),
		env.WithDotenv("/nonexistent/.env"),
		env.WithLookuper(fakeLookuper(map[string]string{"HOST": "fallback"})),
	)
	if err != nil {
		t.Fatalf("expected success when .env file is missing, got: %v", err)
	}
}

// ── WithLookuper nil falls back to OS ──────────────────────────────────────

func TestSetup_lookuper_option_replaces_env(t *testing.T) {
	// Verify that WithLookuper is honoured and we never touch os.Environ.
	called := false
	l := envparse.Lookuper(func(key string) (string, bool) {
		called = true
		if key == "HOST" {
			return "injected", true
		}
		return "", false
	})

	cfg, err := env.Setup[appConfig](context.Background(), env.WithLookuper(l))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("custom lookuper was never called")
	}
	if cfg.Host != "injected" {
		t.Errorf("Host = %q, want injected", cfg.Host)
	}
}
