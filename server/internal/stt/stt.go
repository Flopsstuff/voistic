// Package stt turns recordings into text through the Groq speech-to-text API.
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// DefaultModel is the most accurate model Groq offers.
	DefaultModel = "whisper-large-v3"

	defaultURL      = "https://api.groq.com/openai/v1/audio/transcriptions"
	maxErrorBytes   = 300
	maxAnswerBytes  = 1 << 20
	transcriptExt   = ".txt"
	transcriptPerms = 0o600
)

// Client talks to the API. Language is an ISO 639-1 code, and left empty the API
// guesses it, which goes wrong easily on a few words.
type Client struct {
	APIKey   string
	Model    string
	Language string
	Ignore   []string // phrases to cut from the text
	URL      string   // overrides the API address, for tests
}

// Transcribe returns the text of an audio file.
func (c Client) Transcribe(ctx context.Context, path string) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := addFile(form, path); err != nil {
		return "", err
	}
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	fields := map[string]string{"model": model, "response_format": "json", "temperature": "0"}
	if c.Language != "" {
		fields["language"] = c.Language
	}
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return "", err
		}
	}
	if err := form.Close(); err != nil {
		return "", err
	}

	url := c.URL
	if url == "" {
		url = defaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		if len(answer) > maxErrorBytes {
			answer = answer[:maxErrorBytes]
		}
		return "", fmt.Errorf("groq: %s: %s", resp.Status, answer)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(answer, &result); err != nil {
		return "", fmt.Errorf("groq: %w", err)
	}
	return Clean(result.Text, c.Ignore), nil
}

// Clean cuts the phrases from text, in any case and with the punctuation after
// them. What's left may be nothing.
func Clean(text string, phrases []string) string {
	for _, phrase := range phrases {
		if phrase = strings.TrimSpace(phrase); phrase != "" {
			pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(phrase) + `[\s.,!?…]*`)
			text = pattern.ReplaceAllString(text, "")
		}
	}
	text = strings.TrimSpace(text)
	if strings.Trim(text, " .,!?…-") == "" {
		return ""
	}
	return text
}

// TranscribeFile saves the text next to the recording, as the same name with
// .txt, and returns where it went and the text. With no text nothing is saved
// and the path is empty.
func (c Client) TranscribeFile(ctx context.Context, path string) (string, string, error) {
	text, err := c.Transcribe(ctx, path)
	if err != nil {
		return "", "", err
	}
	if text == "" {
		return "", "", nil
	}
	out := strings.TrimSuffix(path, filepath.Ext(path)) + transcriptExt
	if err := os.WriteFile(out, []byte(text+"\n"), transcriptPerms); err != nil {
		return "", "", err
	}
	return out, text, nil
}

func addFile(form *multipart.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := form.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}
