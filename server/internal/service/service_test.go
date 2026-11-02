package service

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"unicode/utf16"
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

func decodeTask(t *testing.T, data []byte) string {
	t.Helper()
	if !bytes.HasPrefix(data, []byte{0xFF, 0xFE}) {
		t.Fatalf("no byte order mark: % x", data[:2])
	}
	units := make([]uint16, (len(data)-2)/2)
	if err := binary.Read(bytes.NewReader(data[2:]), binary.LittleEndian, units); err != nil {
		t.Fatal(err)
	}
	return string(utf16.Decode(units))
}

func TestTaskEscapesPaths(t *testing.T) {
	text := decodeTask(t, Task(`C:\a&b\voisticw.exe`, `C:\a&b\serve.log`, `PC\кто`))
	for _, want := range []string{
		`<Command>C:\a&amp;b\voisticw.exe</Command>`,
		`<UserId>PC\кто</UserId>`,
		`<LogonType>InteractiveToken</LogonType>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %s in\n%s", want, text)
		}
	}
	// The quotes around the log stay quotes once XML is decoded.
	type task struct {
		Arguments string `xml:"Actions>Exec>Arguments"`
	}
	var parsed task
	decoder := xml.NewDecoder(strings.NewReader(strings.Replace(text, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)))
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatalf("bad xml: %v", err)
	}
	if parsed.Arguments != `serve --log "C:\a&b\serve.log"` {
		t.Errorf("arguments %q", parsed.Arguments)
	}
}
