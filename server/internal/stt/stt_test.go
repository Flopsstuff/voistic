package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscribeFileSavesTextNextToTheRecording(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("auth %q", got)
		}
		if got := r.FormValue("model"); got != DefaultModel {
			t.Errorf("model %q", got)
		}
		if got := r.FormValue("language"); got != "ru" {
			t.Errorf("language %q", got)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if header.Filename != "one.wav" || string(data) != "audio" {
			t.Errorf("file %s: %q", header.Filename, data)
		}
		json.NewEncoder(w).Encode(map[string]string{"text": " привет "})
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "one.wav")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := Client{APIKey: "secret", Language: "ru", URL: server.URL}
	out, text, err := client.TranscribeFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if text != "привет" {
		t.Errorf("text %q", text)
	}
	saved, err := os.ReadFile(strings.TrimSuffix(path, ".wav") + ".txt")
	if err != nil || out != strings.TrimSuffix(path, ".wav")+".txt" {
		t.Fatalf("out %s: %v", out, err)
	}
	if string(saved) != "привет\n" {
		t.Errorf("saved %q", saved)
	}
}

func TestAnErrorDoesNotLeaveAFileOrTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "one.wav")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Client{APIKey: "secret", URL: server.URL}.TranscribeFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "rate limited") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "one.txt")); !os.IsNotExist(err) {
		t.Errorf("left a transcript: %v", err)
	}
}

func TestCleanCutsPhrases(t *testing.T) {
	phrases := []string{"Субтитры сделал DimaTorzok", "Продолжение следует", " "}
	cases := map[string]string{
		"Открой файл. Субтитры сделал DimaTorzok.":         "Открой файл.",
		"субтитры сделал dimatorzok":                       "",
		"Продолжение следует...":                           "",
		"  Обычный текст.  ":                               "Обычный текст.",
		"Продолжение следует... Текст остался.":            "Текст остался.",
		"Субтитры сделал DimaTorzok, Продолжение следует!": "",
	}
	for text, want := range cases {
		if got := Clean(text, phrases); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}

func TestOnlyIgnoredTextSavesNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "Продолжение следует..."})
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "one.wav")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := Client{APIKey: "secret", URL: server.URL, Ignore: []string{"Продолжение следует"}}
	out, text, err := client.TranscribeFile(context.Background(), path)
	if err != nil || out != "" || text != "" {
		t.Fatalf("out %q, text %q, err %v", out, text, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "one.txt")); !os.IsNotExist(err) {
		t.Errorf("left a transcript: %v", err)
	}
}

func TestPromptAndReplacementsReachTheText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.FormValue("prompt"); got != "Проект voistic, ESP32." {
			t.Errorf("prompt %q", got)
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "Войстик на ESP 32 работает."})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "one.wav")
	os.WriteFile(path, []byte("audio"), 0o600)

	client := Client{
		APIKey:  "secret",
		URL:     server.URL,
		Prompt:  "Проект voistic, ESP32.",
		Replace: map[string]string{"войстик": "voistic", "esp 32": "ESP32"},
	}
	text, err := client.Transcribe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if text != "voistic на ESP32 работает." {
		t.Errorf("text %q", text)
	}
}

func TestNoPromptSendsNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		if _, ok := r.MultipartForm.Value["prompt"]; ok {
			t.Error("an empty prompt was sent")
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "ok"})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "one.wav")
	os.WriteFile(path, []byte("audio"), 0o600)
	if _, err := (Client{URL: server.URL}).Transcribe(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceWholeWordsInAnyCase(t *testing.T) {
	replace := map[string]string{
		"клод":     "Claude",
		"клод код": "Claude Code",
		"сяо":      "XIAO",
		"git":      "Git",
	}
	for _, c := range []struct{ in, want string }{
		{"Открой Клод Код и клод.", "Открой Claude Code и Claude."},
		{"КЛОД, привет", "Claude, привет"},
		{"плата СЯО и сяомка", "плата XIAO и сяомка"},
		{"github и git", "github и Git"},
		{"клодклод", "клодклод"},
		{"", ""},
	} {
		if got := Replace(c.in, replace); got != c.want {
			t.Errorf("Replace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
