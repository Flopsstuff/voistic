// Package action runs what a gesture is configured to do.
package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"voistic/server/internal/config"
)

const (
	defaultTimeout = 30 * time.Second
	// How long a finished command's children may hold its output open.
	waitDelay = 2 * time.Second
	// The most output kept for the log.
	maxOutput = 4096
)

var variable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Event says which press triggered an action.
type Event struct {
	Device  string
	Gesture string
}

// Run executes an action and returns its output.
func Run(ctx context.Context, a config.Action, event Event) (string, error) {
	timeout := a.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if a.HTTP != nil {
		return runHTTP(ctx, *a.HTTP)
	}
	return runExec(ctx, a.Exec, event)
}

func runExec(ctx context.Context, argv []string, event Event) (string, error) {
	args := make([]string, len(argv))
	for i, arg := range argv {
		expanded, err := expand(arg)
		if err != nil {
			return "", err
		}
		args[i] = expanded
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "VOISTIC_DEVICE="+event.Device, "VOISTIC_GESTURE="+event.Gesture)
	cmd.WaitDelay = waitDelay
	output, err := cmd.CombinedOutput()
	return tail(output), err
}

func runHTTP(ctx context.Context, h config.HTTP) (string, error) {
	url, err := expand(h.URL)
	if err != nil {
		return "", err
	}
	body, contentType, err := requestBody(h)
	if err != nil {
		return "", err
	}

	method := h.Method
	if method == "" {
		method = http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return "", err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, value := range h.Headers {
		expanded, err := expand(value)
		if err != nil {
			return "", err
		}
		req.Header.Set(name, expanded)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	output, err := io.ReadAll(io.LimitReader(resp.Body, maxOutput))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return string(output), fmt.Errorf("http status %d", resp.StatusCode)
	}
	return string(output), nil
}

func requestBody(h config.HTTP) (body []byte, contentType string, err error) {
	switch {
	case h.JSON != nil:
		expanded, err := expandValue(h.JSON)
		if err != nil {
			return nil, "", err
		}
		body, err = json.Marshal(expanded)
		return body, "application/json", err
	case h.Body != "":
		expanded, err := expand(h.Body)
		return []byte(expanded), "", err
	default:
		return nil, "", nil
	}
}

// expand replaces ${VAR} with its value from the environment. An unset variable
// is an error rather than an empty string, so a misconfigured action fails
// instead of running with a blank token or path. A bare $ stays as it is, which
// keeps shell snippets intact.
func expand(s string) (string, error) {
	var missing string
	expanded := variable.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		value, ok := os.LookupEnv(name)
		if !ok {
			missing = name
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is not set", missing)
	}
	return expanded, nil
}

func expandValue(value any) (any, error) {
	switch v := value.(type) {
	case string:
		return expand(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			expanded, err := expandValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = expanded
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			expanded, err := expandValue(item)
			if err != nil {
				return nil, err
			}
			out[key] = expanded
		}
		return out, nil
	default:
		return value, nil
	}
}

func tail(output []byte) string {
	if len(output) > maxOutput {
		output = output[len(output)-maxOutput:]
	}
	return strings.TrimSpace(string(output))
}
