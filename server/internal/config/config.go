// Package config loads the hand-edited YAML file that maps button gestures to
// actions.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"voistic/server/internal/paste"
)

// DefaultListen is the address used when the config doesn't set one.
const DefaultListen = ":8787"

// Gestures lists every gesture a button can send.
var Gestures = []string{"single", "double", "triple", "long"}

// Config is the parsed config file.
type Config struct {
	Listen     string `yaml:"listen"`
	Recordings string `yaml:"recordings"` // folder for voice recordings
	// SaveRecordings, off, drops the audio and the text once they've been used.
	// Unset means on.
	SaveRecordings *bool `yaml:"save_recordings"`
	Paste          bool  `yaml:"paste"` // type transcripts into the active window
	// PasteSuffix goes after each pasted transcript, such as a space.
	PasteSuffix string            `yaml:"paste_suffix"`
	Groq        Groq              `yaml:"groq"`
	Devices     map[string]Device `yaml:"devices"`
}

// Groq sets up speech recognition through the Groq API.
// The file is hand-edited but holds this secret, so it should be mode 600.
type Groq struct {
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`    // empty means the default model
	Language string `yaml:"language"` // ISO 639-1, empty lets the API guess
	// Ignore lists phrases to cut from transcripts, in any case. Whisper makes
	// them up from its training data, usually over silence.
	Ignore []string `yaml:"ignore"`
}

// Device holds the actions of one paired button.
type Device struct {
	Name     string            `yaml:"name"`
	Gestures map[string]Action `yaml:"gestures"`
	// How long a button that can stays connected after a press before deep
	// sleep, see AwakeFor.
	DeepSleepAfter time.Duration `yaml:"deep_sleep_after"`
}

// DefaultDeepSleepAfter is how long a button stays connected when the config
// doesn't say.
const DefaultDeepSleepAfter = 30 * time.Minute

// AwakeFor is how long the device stays connected after a press. Only a board
// that can keep Wi-Fi in light sleep uses it, the XIAO always sleeps at once.
func (c *Config) AwakeFor(device string) time.Duration {
	if after := c.Devices[device].DeepSleepAfter; after > 0 {
		return after
	}
	return DefaultDeepSleepAfter
}

// Action is what a gesture does. Exactly one of Exec, HTTP and Key is set.
type Action struct {
	Exec    []string      `yaml:"exec"`
	HTTP    *HTTP         `yaml:"http"`
	Key     string        `yaml:"key"` // a key to press, on macOS and Windows
	Timeout time.Duration `yaml:"timeout"`
}

// HTTP describes a request to send.
type HTTP struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`
	JSON    any               `yaml:"json"`
	Body    string            `yaml:"body"`
}

// KeepsRecordings reports whether recordings stay on disk.
func (c *Config) KeepsRecordings() bool {
	return c.SaveRecordings == nil || *c.SaveRecordings
}

// Action returns the action bound to a gesture of a device, if any.
func (c *Config) Action(device, gesture string) (Action, bool) {
	action, ok := c.Devices[device].Gestures[gesture]
	return action, ok
}

// Load reads and validates the config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	for id, device := range c.Devices {
		if device.DeepSleepAfter < 0 {
			return fmt.Errorf("device %s: deep_sleep_after can't be negative", id)
		}
		for gesture, action := range device.Gestures {
			if !isGesture(gesture) {
				return fmt.Errorf("device %s: unknown gesture %q", id, gesture)
			}
			if err := action.validate(); err != nil {
				return fmt.Errorf("device %s, gesture %s: %w", id, gesture, err)
			}
		}
	}
	return nil
}

func (a Action) validate() error {
	kinds := 0
	for _, set := range []bool{len(a.Exec) > 0, a.HTTP != nil, a.Key != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return errors.New("set exactly one of exec, http and key")
	}
	if a.Key != "" && !paste.IsKey(a.Key) {
		return fmt.Errorf("unknown key %q, use one of %v", a.Key, paste.Keys)
	}
	if a.Timeout < 0 {
		return errors.New("timeout can't be negative")
	}
	if a.HTTP == nil {
		return nil
	}
	if a.HTTP.URL == "" {
		return errors.New("http needs a url")
	}
	if a.HTTP.JSON != nil && a.HTTP.Body != "" {
		return errors.New("set only one of http json and body")
	}
	// A ${VAR} host is only known at run time, so only the shape is checked.
	if _, err := url.Parse(a.HTTP.URL); err != nil {
		return fmt.Errorf("http url: %w", err)
	}
	return nil
}

func isGesture(name string) bool {
	for _, gesture := range Gestures {
		if gesture == name {
			return true
		}
	}
	return false
}

// Loader re-reads the file on every call, so edits apply without a restart.
type Loader struct {
	path string
	log  *slog.Logger

	mu   sync.Mutex
	last *Config
}

// NewLoader returns a Loader for path.
func NewLoader(path string, log *slog.Logger) *Loader {
	return &Loader{path: path, log: log}
}

// Get returns the current config. A missing file counts as an empty config, so
// the server can pair a first device before anything is configured. A broken
// file is logged and the last valid config stays in use.
func (l *Loader) Get() (*Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cfg, err := Load(l.path)
	switch {
	case err == nil:
		l.last = cfg
		return cfg, nil
	case errors.Is(err, os.ErrNotExist):
		l.last = &Config{Listen: DefaultListen}
		return l.last, nil
	case l.last != nil:
		l.log.Error("config is broken, keeping the last valid version", "err", err)
		return l.last, nil
	default:
		return nil, err
	}
}
