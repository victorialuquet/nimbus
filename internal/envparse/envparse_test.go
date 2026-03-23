package envparse

import (
	"testing"
	"time"
)

// fakeLookuper builds a Lookuper from a static key→value map.
func fakeLookuper(kv map[string]string) Lookuper {
	return func(key string) (string, bool) {
		v, ok := kv[key]
		return v, ok
	}
}

// ── Process — non-struct input ─────────────────────────────────────────────

func TestProcess_nonPointer(t *testing.T) {
	var s struct {
		X string `env:"X"`
	}
	if err := Process(s, fakeLookuper(nil)); err == nil {
		t.Error("expected error for non-pointer dst, got nil")
	}
}

func TestProcess_pointerToNonStruct(t *testing.T) {
	n := 42
	if err := Process(&n, fakeLookuper(nil)); err == nil {
		t.Error("expected error for pointer-to-non-struct, got nil")
	}
}

// ── String field ───────────────────────────────────────────────────────────

func TestProcess_string(t *testing.T) {
	type C struct {
		Name string `env:"NAME"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"NAME": "alice"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Name != "alice" {
		t.Errorf("Name = %q, want alice", c.Name)
	}
}

func TestProcess_string_missing_optional(t *testing.T) {
	type C struct {
		Name string `env:"NAME"`
	}
	var c C
	if err := Process(&c, fakeLookuper(nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Name != "" {
		t.Errorf("Name = %q, want empty", c.Name)
	}
}

// ── Required ───────────────────────────────────────────────────────────────

func TestProcess_required_missing(t *testing.T) {
	type C struct {
		Token string `env:"TOKEN,required"`
	}
	var c C
	if err := Process(&c, fakeLookuper(nil)); err == nil {
		t.Error("expected error for missing required field, got nil")
	}
}

func TestProcess_required_present(t *testing.T) {
	type C struct {
		Token string `env:"TOKEN,required"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"TOKEN": "abc"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Token != "abc" {
		t.Errorf("Token = %q, want abc", c.Token)
	}
}

func TestProcess_required_empty_string(t *testing.T) {
	// An empty string is treated the same as missing for required fields.
	type C struct {
		Token string `env:"TOKEN,required"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"TOKEN": ""})); err == nil {
		t.Error("expected error for empty required field, got nil")
	}
}

// ── Default ────────────────────────────────────────────────────────────────

func TestProcess_default_used_when_missing(t *testing.T) {
	type C struct {
		Port string `env:"PORT,default=8080"`
	}
	var c C
	if err := Process(&c, fakeLookuper(nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080", c.Port)
	}
}

func TestProcess_default_not_used_when_present(t *testing.T) {
	type C struct {
		Port string `env:"PORT,default=8080"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"PORT": "9090"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != "9090" {
		t.Errorf("Port = %q, want 9090", c.Port)
	}
}

// ── Bool ───────────────────────────────────────────────────────────────────

func TestProcess_bool(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
	}
	for _, tt := range tests {
		type C struct {
			Debug bool `env:"DEBUG"`
		}
		var c C
		if err := Process(&c, fakeLookuper(map[string]string{"DEBUG": tt.raw})); err != nil {
			t.Errorf("raw=%q: unexpected error: %v", tt.raw, err)
			continue
		}
		if c.Debug != tt.want {
			t.Errorf("raw=%q: Debug = %v, want %v", tt.raw, c.Debug, tt.want)
		}
	}
}

func TestProcess_bool_invalid(t *testing.T) {
	type C struct {
		Debug bool `env:"DEBUG"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"DEBUG": "yes"})); err == nil {
		t.Error("expected error for invalid bool, got nil")
	}
}

// ── Int variants ───────────────────────────────────────────────────────────

func TestProcess_int(t *testing.T) {
	type C struct {
		Port int `env:"PORT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"PORT": "3000"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != 3000 {
		t.Errorf("Port = %d, want 3000", c.Port)
	}
}

func TestProcess_int_invalid(t *testing.T) {
	type C struct {
		Port int `env:"PORT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"PORT": "abc"})); err == nil {
		t.Error("expected error for invalid int, got nil")
	}
}

func TestProcess_int64(t *testing.T) {
	type C struct {
		Max int64 `env:"MAX"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"MAX": "9223372036854775807"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Max != 9223372036854775807 {
		t.Errorf("Max = %d, want max int64", c.Max)
	}
}

// ── Uint variants ──────────────────────────────────────────────────────────

func TestProcess_uint(t *testing.T) {
	type C struct {
		Count uint `env:"COUNT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"COUNT": "42"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Count != 42 {
		t.Errorf("Count = %d, want 42", c.Count)
	}
}

func TestProcess_uint_negative(t *testing.T) {
	type C struct {
		Count uint `env:"COUNT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"COUNT": "-1"})); err == nil {
		t.Error("expected error for negative uint, got nil")
	}
}

func TestProcess_uint32(t *testing.T) {
	type C struct {
		Val uint32 `env:"VAL"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"VAL": "4294967295"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Val != 4294967295 {
		t.Errorf("Val = %d, want 4294967295", c.Val)
	}
}

// ── Float variants ─────────────────────────────────────────────────────────

func TestProcess_float64(t *testing.T) {
	type C struct {
		Rate float64 `env:"RATE"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"RATE": "3.14"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Rate != 3.14 {
		t.Errorf("Rate = %f, want 3.14", c.Rate)
	}
}

func TestProcess_float32(t *testing.T) {
	type C struct {
		Rate float32 `env:"RATE"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"RATE": "1.5"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Rate != 1.5 {
		t.Errorf("Rate = %f, want 1.5", c.Rate)
	}
}

func TestProcess_float_invalid(t *testing.T) {
	type C struct {
		Rate float64 `env:"RATE"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"RATE": "fast"})); err == nil {
		t.Error("expected error for invalid float, got nil")
	}
}

// ── time.Duration ──────────────────────────────────────────────────────────

func TestProcess_duration(t *testing.T) {
	type C struct {
		Timeout time.Duration `env:"TIMEOUT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"TIMEOUT": "5s"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", c.Timeout)
	}
}

func TestProcess_duration_invalid(t *testing.T) {
	type C struct {
		Timeout time.Duration `env:"TIMEOUT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"TIMEOUT": "5"})); err == nil {
		t.Error("expected error for bare integer duration, got nil")
	}
}

// ── TextUnmarshaler ────────────────────────────────────────────────────────

// customIP is a simple type that implements encoding.TextUnmarshaler.
type customIP struct {
	raw string
}

func (c *customIP) UnmarshalText(b []byte) error {
	c.raw = string(b)
	return nil
}

func TestProcess_textUnmarshaler(t *testing.T) {
	type C struct {
		IP customIP `env:"IP"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"IP": "192.0.2.1"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.IP.raw != "192.0.2.1" {
		t.Errorf("IP.raw = %q, want 192.0.2.1", c.IP.raw)
	}
}

// ── Ignored fields ─────────────────────────────────────────────────────────

func TestProcess_dash_tag_ignored(t *testing.T) {
	type C struct {
		Skip string `env:"-"`
	}
	var c C
	// Even if the env var is set, it should not be parsed.
	if err := Process(&c, fakeLookuper(map[string]string{"-": "oops"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Skip != "" {
		t.Errorf("Skip = %q, want empty (should be ignored)", c.Skip)
	}
}

func TestProcess_no_tag_ignored(t *testing.T) {
	type C struct {
		Internal string
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"Internal": "x"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Internal != "" {
		t.Errorf("Internal = %q, want empty (untagged field should be ignored)", c.Internal)
	}
}

// ── Embedded / nested structs ──────────────────────────────────────────────

func TestProcess_embedded_struct(t *testing.T) {
	type Base struct {
		Host string `env:"HOST"`
	}
	type C struct {
		Base
		Port int `env:"PORT"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{
		"HOST": "localhost",
		"PORT": "5432",
	})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", c.Host)
	}
	if c.Port != 5432 {
		t.Errorf("Port = %d, want 5432", c.Port)
	}
}

func TestProcess_nested_struct_without_tag(t *testing.T) {
	type DB struct {
		URL string `env:"DB_URL"`
	}
	type C struct {
		DB DB
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"DB_URL": "postgres://localhost/test"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.DB.URL != "postgres://localhost/test" {
		t.Errorf("DB.URL = %q, want postgres://localhost/test", c.DB.URL)
	}
}

// ── Unsupported type ───────────────────────────────────────────────────────

func TestProcess_unsupported_type(t *testing.T) {
	type C struct {
		Ch chan int `env:"CH"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{"CH": "x"})); err == nil {
		t.Error("expected error for unsupported field type, got nil")
	}
}

// ── nil lookuper falls back to OSLookuper ──────────────────────────────────

func TestProcess_nil_lookuper_uses_OS(t *testing.T) {
	// Patch OSLookuper to a fake.
	orig := OSLookuper
	OSLookuper = fakeLookuper(map[string]string{"APP": "nimbus"})
	t.Cleanup(func() { OSLookuper = orig })

	type C struct {
		App string `env:"APP"`
	}
	var c C
	if err := Process(&c, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.App != "nimbus" {
		t.Errorf("App = %q, want nimbus", c.App)
	}
}

// ── Multiple fields at once ────────────────────────────────────────────────

func TestProcess_multiple_fields(t *testing.T) {
	type C struct {
		Host    string        `env:"HOST,required"`
		Port    int           `env:"PORT,default=5432"`
		Debug   bool          `env:"DEBUG,default=false"`
		Timeout time.Duration `env:"TIMEOUT,default=30s"`
	}
	var c C
	if err := Process(&c, fakeLookuper(map[string]string{
		"HOST":  "db.example.com",
		"PORT":  "5433",
		"DEBUG": "true",
	})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Host != "db.example.com" {
		t.Errorf("Host = %q, want db.example.com", c.Host)
	}
	if c.Port != 5433 {
		t.Errorf("Port = %d, want 5433", c.Port)
	}
	if !c.Debug {
		t.Error("Debug = false, want true")
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s (default)", c.Timeout)
	}
}
