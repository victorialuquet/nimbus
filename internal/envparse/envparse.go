// Package envparse provides a minimal environment variable parser that
// populates a struct from os.Environ using struct field tags.
//
// Supported tags (on the `env` struct tag):
//
//	env:"KEY"              - maps field to $KEY
//	env:"KEY,required"     - error if $KEY is unset or empty
//	env:"KEY,default=val"  - use val when $KEY is unset
//
// Types supported: string, bool, int, int64, float64, time.Duration,
// and any type implementing encoding.TextUnmarshaler.
package envparse

import (
	"encoding"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Lookuper is a function that resolves an environment variable by name.
// The default implementation wraps [os.LookupEnv]; override for testing.
type Lookuper func(key string) (string, bool)

// OSLookuper is the default [Lookuper] backed by [os.LookupEnv].
var OSLookuper Lookuper = os.LookupEnv

// Process populates dst (must be a non-nil pointer to a struct) from
// environment variables, using the provided lookuper.
func Process(dst any, lookup Lookuper) error {
	if lookup == nil {
		lookup = OSLookuper
	}

	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("envparse: dst must be a non-nil pointer to a struct")
	}
	return processStruct(v.Elem(), lookup)
}

func processStruct(v reflect.Value, lookup Lookuper) error {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		fv := v.Field(i)

		if !fv.CanSet() {
			continue
		}

		// Recurse into embedded / nested structs without an env tag.
		if field.Anonymous || (fv.Kind() == reflect.Struct && field.Tag.Get("env") == "") {
			if err := processStruct(fv, lookup); err != nil {
				return err
			}
			continue
		}

		tag := field.Tag.Get("env")
		if tag == "" || tag == "-" {
			continue
		}

		key, opts := parseTag(tag)
		raw, found := lookup(key)

		if !found || raw == "" {
			if opts.required {
				return fmt.Errorf("envparse: required variable %q is not set", key)
			}
			if opts.defaultVal != "" {
				raw = opts.defaultVal
				found = true
			}
		}

		if !found {
			continue
		}

		if err := setField(fv, raw, key); err != nil {
			return err
		}
	}
	return nil
}

type tagOpts struct {
	required   bool
	defaultVal string
}

func parseTag(tag string) (key string, opts tagOpts) {
	parts := strings.SplitN(tag, ",", -1)
	key = parts[0]
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		switch {
		case p == "required":
			opts.required = true
		case strings.HasPrefix(p, "default="):
			opts.defaultVal = strings.TrimPrefix(p, "default=")
		}
	}
	return
}

func setField(fv reflect.Value, raw, key string) error {
	// TextUnmarshaler takes priority.
	if fv.CanAddr() {
		if u, ok := fv.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText([]byte(raw))
		}
	}

	//nolint:exhaustive — unsupported kinds are caught by the default branch.
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("envparse: %s: cannot parse %q as bool", key, raw)
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Special-case time.Duration.
		if fv.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("envparse: %s: cannot parse %q as duration", key, raw)
			}
			fv.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("envparse: %s: cannot parse %q as int", key, raw)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("envparse: %s: cannot parse %q as uint", key, raw)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("envparse: %s: cannot parse %q as float", key, raw)
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("envparse: %s: unsupported field type %s", key, fv.Type())
	}
	return nil
}
