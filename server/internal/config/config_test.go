package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, `
devices:
  voistic-8cbc:
    name: Bedside
    gestures:
      single:
        http:
          url: http://example.com/a
          json: {line: wake}
      long:
        exec: [say, done]
        timeout: 5s
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	action, ok := cfg.Action("voistic-8cbc", "long")
	if !ok || action.Timeout != 5*time.Second || len(action.Exec) != 2 {
		t.Errorf("long action = %+v, %v", action, ok)
	}
	if _, ok := cfg.Action("voistic-8cbc", "double"); ok {
		t.Error("double should be unbound")
	}
	if _, ok := cfg.Action("nobody", "single"); ok {
		t.Error("unknown device should have no action")
	}
}

func TestLoadRejects(t *testing.T) {
	const device = "devices:\n  a:\n    gestures:\n"
	tests := map[string]struct{ yaml, want string }{
		"unknown gesture": {device + "      quad: {exec: [x]}\n", "unknown gesture"},
		"both kinds":      {device + "      single: {exec: [x], http: {url: http://a}}\n", "exactly one"},
		"key and exec":    {device + "      single: {exec: [x], key: tab}\n", "exactly one"},
		"unknown key":     {device + "      single: {key: f13}\n", "unknown key"},
		"neither kind":    {device + "      single: {timeout: 1s}\n", "exactly one"},
		"http no url":     {device + "      single: {http: {method: GET}}\n", "needs a url"},
		"json and body":   {device + "      single: {http: {url: http://a, json: {}, body: x}}\n", "only one"},
		"unknown field":   {"listne: \":1\"\n", "listne"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			write(t, path, test.yaml)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("err = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestLoaderAppliesEditsAndKeepsLastValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	loader := NewLoader(path, slog.New(slog.DiscardHandler))

	cfg, err := loader.Get()
	if err != nil || cfg.Listen != DefaultListen {
		t.Fatalf("missing file: cfg = %+v, err = %v", cfg, err)
	}

	write(t, path, "listen: \":1234\"\n")
	if cfg, err = loader.Get(); err != nil || cfg.Listen != ":1234" {
		t.Fatalf("edit not applied: cfg = %+v, err = %v", cfg, err)
	}

	write(t, path, "listen: [broken\n")
	if cfg, err = loader.Get(); err != nil || cfg.Listen != ":1234" {
		t.Fatalf("broken file: cfg = %+v, err = %v", cfg, err)
	}
}

func TestLoaderFailsWhenFirstLoadIsBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write(t, path, "listen: [broken\n")
	if _, err := NewLoader(path, slog.New(slog.DiscardHandler)).Get(); err == nil {
		t.Fatal("want an error")
	}
}

func TestLoadReadsGroqKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("groq:\n  api_key: test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Groq.APIKey != "test-key" {
		t.Errorf("api key %q", cfg.Groq.APIKey)
	}
}

func TestRecordingsAreKeptUnlessTurnedOff(t *testing.T) {
	for text, want := range map[string]bool{"": true, "save_recordings: true\n": true, "save_recordings: false\n": false} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.KeepsRecordings(); got != want {
			t.Errorf("%q: got %v", text, got)
		}
	}
}
