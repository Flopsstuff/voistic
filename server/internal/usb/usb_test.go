package usb

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

// fakePort answers each line written to it from a script, as a board would.
type fakePort struct {
	serial.Port
	answers map[string][]string
	out     *io.PipeWriter
	in      *io.PipeReader
	written []string
}

func newFake(answers map[string][]string) *fakePort {
	in, out := io.Pipe()
	return &fakePort{answers: answers, in: in, out: out}
}

func (f *fakePort) Read(p []byte) (int, error) { return f.in.Read(p) }
func (f *fakePort) Close() error               { return f.out.Close() }

func (f *fakePort) Write(p []byte) (int, error) {
	line := strings.TrimSuffix(string(p), "\n")
	f.written = append(f.written, line)
	go func() {
		for _, answer := range f.answers[line] {
			f.out.Write([]byte(answer + "\r\n"))
		}
	}()
	return len(p), nil
}

func board(t *testing.T, answers map[string][]string) (*Board, *fakePort, *[]string) {
	t.Helper()
	fake := newFake(answers)
	var logged []string
	b := &Board{port: fake, lines: make(chan string, 16), Log: func(line string) { logged = append(logged, line) }}
	go b.read()
	t.Cleanup(func() { fake.Close() })
	return b, fake, &logged
}

func TestCommandSkipsLogLines(t *testing.T) {
	b, _, logged := board(t, map[string][]string{
		"brightness 0.30": {"wifi: connected in 400 ms", "brightness: saved 0.30"},
	})
	if err := b.SetBrightness(context.Background(), 0.3); err != nil {
		t.Fatal(err)
	}
	if len(*logged) != 1 || (*logged)[0] != "wifi: connected in 400 ms" {
		t.Errorf("logged = %q", *logged)
	}
}

func TestCommandError(t *testing.T) {
	b, _, _ := board(t, map[string][]string{"pair k h p": {"error: pair key host port"}})
	err := b.Adopt(context.Background(), "k", "h", "p")
	if err == nil || err.Error() != "pair key host port" {
		t.Errorf("err = %v", err)
	}
}

func TestCommandTimesOut(t *testing.T) {
	b, _, _ := board(t, nil)
	if _, err := b.Command(context.Background(), "status", 50*time.Millisecond); err == nil {
		t.Error("a silent board should time out")
	}
}

func TestHelloReadsStatus(t *testing.T) {
	b, _, _ := board(t, map[string][]string{"hello": {
		`hello: {"device":"voistic-25bc","board":"iot-button-c6","fw":"v1","paired":true,"server":"10.0.0.2:8787","ssid":"Home","connected":true,"ip":"10.0.0.9","rssi":-50,"battery_mv":3900,"brightness":0.20}`,
	}})
	status, err := b.hello(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Status{Device: "voistic-25bc", Board: "iot-button-c6", Firmware: "v1", Paired: true, Server: "10.0.0.2:8787",
		SSID: "Home", Connected: true, IP: "10.0.0.9", RSSI: -50, Battery: 3900, Brightness: 0.2}
	if status != want {
		t.Errorf("status = %+v", status)
	}
}

func TestSetWiFiRefusesSeparator(t *testing.T) {
	b, fake, _ := board(t, nil)
	if err := b.SetWiFi(context.Background(), "a|b", "x"); err == nil {
		t.Error("a | in the name should be refused")
	}
	if len(fake.written) != 0 {
		t.Errorf("sent %q", fake.written)
	}
}

func TestGoneBoard(t *testing.T) {
	b, fake, _ := board(t, nil)
	fake.Close()
	if _, err := b.Command(context.Background(), "status", time.Second); err != ErrGone {
		t.Errorf("err = %v", err)
	}
}
