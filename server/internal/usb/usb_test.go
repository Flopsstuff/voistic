package usb

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"slices"
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

// fakeRom is a chip in its ROM loader: it answers a sync eight times over,
// reads its registers out and takes writes, and goes off USB once its
// watchdog is switched on.
type fakeRom struct {
	regs    map[uint32]uint32
	writes  [][2]uint32
	in      []byte // from the host, not taken apart yet
	out     []byte
	gone    bool
	garbage string // what a firmware would have printed first
}

func (f *fakeRom) SetReadTimeout(time.Duration) error { return nil }

func (f *fakeRom) Read(p []byte) (int, error) {
	if len(f.out) == 0 {
		if f.gone {
			return 0, io.ErrClosedPipe
		}
		time.Sleep(time.Millisecond)
		return 0, nil
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

func (f *fakeRom) Write(p []byte) (int, error) {
	if f.gone {
		return 0, io.ErrClosedPipe
	}
	f.in = append(f.in, p...)
	for {
		start := bytes.IndexByte(f.in, 0xC0)
		if start < 0 {
			return len(p), nil
		}
		end := bytes.IndexByte(f.in[start+1:], 0xC0)
		if end < 0 {
			return len(p), nil
		}
		packet := f.in[start+1 : start+1+end]
		f.in = f.in[start+end+2:]
		packet = bytes.ReplaceAll(packet, []byte{0xDB, 0xDC}, []byte{0xC0})
		packet = bytes.ReplaceAll(packet, []byte{0xDB, 0xDD}, []byte{0xDB})
		f.handle(packet[1], packet[8:])
	}
}

func (f *fakeRom) handle(op byte, data []byte) {
	answer := func(value uint32) {
		packet := []byte{0x01, op, 4, 0}
		packet = binary.LittleEndian.AppendUint32(packet, value)
		f.out = append(f.out, slip(append(packet, 0, 0, 0, 0))...)
	}
	switch op {
	case loaderSync:
		f.out = append(f.out, f.garbage...)
		for range 8 {
			answer(0x20120707)
		}
	case loaderReadReg:
		answer(f.regs[binary.LittleEndian.Uint32(data)])
	case loaderWriteReg:
		addr, value := binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint32(data[4:])
		f.writes = append(f.writes, [2]uint32{addr, value})
		answer(0)
		if addr == s3WdtConfig0 && value&(1<<31) != 0 {
			f.gone = true
		}
	}
}

func TestLeavesLoaderOnS3(t *testing.T) {
	rom := &fakeRom{regs: map[uint32]uint32{chipMagicReg: chipMagicS3}, garbage: "ESP-ROM:esp32s3\r\n"}
	found, left := (&loader{port: rom}).leave()
	if !found || !left {
		t.Fatalf("found %v, left %v", found, left)
	}
	want := [][2]uint32{{s3WdtProtect, wdtKey}, {s3WdtConfig1, wdtTimeout}, {s3WdtConfig0, wdtEnable}}
	if !slices.Equal(rom.writes, want) {
		t.Errorf("writes = %x", rom.writes)
	}
}

func TestLeavesOtherChipsToTheRestart(t *testing.T) {
	rom := &fakeRom{regs: map[uint32]uint32{chipMagicReg: 0x2CE0806F}} // an ESP32-C6
	found, left := (&loader{port: rom}).leave()
	if !found || left {
		t.Errorf("found %v, left %v", found, left)
	}
	if len(rom.writes) != 0 {
		t.Errorf("writes = %x", rom.writes)
	}
}

// silent is a firmware, which has nothing to say to a sync.
type silent struct{ written int }

func (s *silent) SetReadTimeout(time.Duration) error { return nil }
func (s *silent) Write(p []byte) (int, error)        { s.written += len(p); return len(p), nil }
func (s *silent) Read(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	return copy(p, "wifi: connected in 400 ms\r\n"), nil
}

func TestFirmwareIsNoLoader(t *testing.T) {
	port := &silent{}
	if found, left := (&loader{port: port}).leave(); found || left {
		t.Errorf("found %v, left %v", found, left)
	}
	if port.written != 46 {
		t.Errorf("sent %d bytes, want one sync of 46", port.written)
	}
}

// 0xC0 and 0xDB in a value have to survive the framing.
func TestLoaderReadsEscapedValues(t *testing.T) {
	rom := &fakeRom{regs: map[uint32]uint32{0x60008038: 0xC0DBC0DB}}
	l := &loader{port: rom}
	if value, ok := l.readReg(0x60008038); !ok || value != 0xC0DBC0DB {
		t.Errorf("value = %x, %v", value, ok)
	}
}
