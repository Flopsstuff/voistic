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
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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
	// Prompt tells the model the words to expect and how to spell them, up to
	// 224 tokens. It guides, it doesn't instruct.
	Prompt string
	// Replace swaps words and phrases the model still gets wrong for how they're
	// spelled, see Replace.
	Replace map[string]string
	URL     string // overrides the API address, for tests
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
	if prompt := strings.TrimSpace(c.Prompt); prompt != "" {
		fields["prompt"] = prompt
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
	return Replace(Clean(result.Text, c.Ignore), c.Replace), nil
}

// Replace swaps whole words and phrases, in any case, for how the config
// spells them. A match inside a longer word is left alone, and longer phrases
// go first, so "клод код" wins over "клод".
func Replace(text string, replacements map[string]string) string {
	phrases := make([]string, 0, len(replacements))
	for phrase := range replacements {
		if strings.TrimSpace(phrase) != "" {
			phrases = append(phrases, phrase)
		}
	}
	slices.SortFunc(phrases, func(a, b string) int {
		if diff := utf8.RuneCountInString(b) - utf8.RuneCountInString(a); diff != 0 {
			return diff
		}
		return strings.Compare(a, b)
	})
	for _, phrase := range phrases {
		pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(strings.TrimSpace(phrase)))
		var out strings.Builder
		last := 0
		for _, match := range pattern.FindAllStringIndex(text, -1) {
			if !wholeWord(text, match[0], match[1]) {
				continue
			}
			out.WriteString(text[last:match[0]])
			out.WriteString(replacements[phrase])
			last = match[1]
		}
		out.WriteString(text[last:])
		text = out.String()
	}
	return text
}

// wholeWord reports whether text[start:end] has no letter or digit right
// before or after it. Go's \b only knows ASCII, so it can't tell for Cyrillic.
func wholeWord(text string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	return !isWord(before) && !isWord(after)
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
