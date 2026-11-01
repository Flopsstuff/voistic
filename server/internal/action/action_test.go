package action

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"voistic/server/internal/config"
)

var event = Event{Device: "dev", Gesture: "double"}

func TestExecPassesEventAndExpandsVariables(t *testing.T) {
	t.Setenv("VOISTIC_TEST_VALUE", "hello")
	a := config.Action{Exec: []string{"sh", "-c", `echo ${VOISTIC_TEST_VALUE} $VOISTIC_DEVICE $VOISTIC_GESTURE`}}

	output, err := Run(context.Background(), a, event)
	if err != nil {
		t.Fatal(err)
	}
	if output != "hello dev double" {
		t.Errorf("output = %q", output)
	}
}

func TestExecDoesNotUseAShell(t *testing.T) {
	output, err := Run(context.Background(), config.Action{Exec: []string{"echo", "a; echo b"}}, event)
	if err != nil {
		t.Fatal(err)
	}
	if output != "a; echo b" {
		t.Errorf("output = %q", output)
	}
}

func TestExecFailure(t *testing.T) {
	output, err := Run(context.Background(), config.Action{Exec: []string{"sh", "-c", "echo oops; exit 3"}}, event)
	if err == nil {
		t.Fatal("want an error")
	}
	if output != "oops" {
		t.Errorf("output = %q", output)
	}
}

func TestExecTimeout(t *testing.T) {
	start := time.Now()
	a := config.Action{Exec: []string{"sleep", "30"}, Timeout: 50 * time.Millisecond}
	if _, err := Run(context.Background(), a, event); err == nil {
		t.Fatal("want an error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v", elapsed)
	}
}

func TestMissingVariableFailsWithoutRunning(t *testing.T) {
	a := config.Action{Exec: []string{"sh", "-c", "echo ran ${VOISTIC_TEST_UNSET}"}}
	output, err := Run(context.Background(), a, event)
	if err == nil || !strings.Contains(err.Error(), "VOISTIC_TEST_UNSET") {
		t.Fatalf("err = %v", err)
	}
	if output != "" {
		t.Errorf("output = %q, the command must not run", output)
	}
}

func TestHTTPSendsExpandedRequest(t *testing.T) {
	t.Setenv("VOISTIC_TEST_TOKEN", "secret")

	var method, auth, contentType string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, auth, contentType = r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &body)
		io.WriteString(w, "done")
	}))
	defer server.Close()

	a := config.Action{HTTP: &config.HTTP{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer ${VOISTIC_TEST_TOKEN}"},
		JSON:    map[string]any{"line": "wake ${VOISTIC_TEST_TOKEN}", "n": 1},
	}}
	output, err := Run(context.Background(), a, event)
	if err != nil {
		t.Fatal(err)
	}
	if output != "done" || method != http.MethodPost || auth != "Bearer secret" || contentType != "application/json" {
		t.Errorf("output=%q method=%q auth=%q type=%q", output, method, auth, contentType)
	}
	if body["line"] != "wake secret" || body["n"] != float64(1) {
		t.Errorf("body = %v", body)
	}
}

func TestHTTPDefaultsToGetWithoutBody(t *testing.T) {
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
	}))
	defer server.Close()

	if _, err := Run(context.Background(), config.Action{HTTP: &config.HTTP{URL: server.URL}}, event); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet {
		t.Errorf("method = %q", method)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := Run(context.Background(), config.Action{HTTP: &config.HTTP{URL: server.URL}}, event)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}
