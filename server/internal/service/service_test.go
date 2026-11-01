package service

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestPlistIsValidXmlAndEscapesPaths(t *testing.T) {
	data, err := Plist("/Users/a&b/voistic", "/Users/a&b/serve.log")
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var strs []string
	inString := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("bad xml: %v", err)
		}
		switch tok := token.(type) {
		case xml.StartElement:
			inString = tok.Name.Local == "string"
		case xml.EndElement:
			inString = false
		case xml.CharData:
			if inString {
				strs = append(strs, string(tok))
			}
		}
	}
	joined := strings.Join(strs, "|")
	for _, want := range []string{Label, "/Users/a&b/voistic", "serve", "/Users/a&b/serve.log"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %q in %q", want, joined)
		}
	}
}
