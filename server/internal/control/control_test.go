package control

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"voistic/server/internal/config"
	"voistic/server/internal/firmware"
	"voistic/server/internal/pairing"
	"voistic/server/internal/store"
)

func start(t *testing.T) (*Client, *pairing.Window, string) {
	t.Helper()
	// Unix socket paths have a short length limit, so t.TempDir is too long here.
	dir, err := os.MkdirTemp("", "voistic")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	s, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("dev", store.Device{Key: "00", Seq: 3}); err != nil {
		t.Fatal(err)
	}

	window := &pairing.Window{}
	server := &Server{
		Firmware: firmware.Store{Dir: filepath.Join(dir, "firmware")},
		Refresh:  func() string { return "192.0.2.1:8787" },
		Config: func() (*config.Config, error) {
			return &config.Config{Devices: map[string]config.Device{
				"dev": {Name: "Bedside", Gestures: map[string]config.Action{
					"single": {Exec: []string{"sh", "-c", "echo pressed $VOISTIC_GESTURE"}},
					"double": {Exec: []string{"false"}},
				}},
			}}, nil
		},
		Store:   s,
		Pairing: window,
		Log:     slog.New(slog.DiscardHandler),
	}

	path := filepath.Join(dir, "k.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, path) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	for deadline := time.Now().Add(2 * time.Second); ; {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket didn't appear")
		}
		time.Sleep(time.Millisecond)
	}
	return NewClient(path), window, path
}

func TestTestRunsAction(t *testing.T) {
	client, _, _ := start(t)
	result, err := client.Test("dev", "single")
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "pressed single" || result.Error != "" {
		t.Errorf("result = %+v", result)
	}
}

func TestTestReportsActionFailure(t *testing.T) {
	client, _, _ := start(t)
	result, err := client.Test("dev", "double")
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" {
		t.Error("want the action's error")
	}
}

func TestTestUnconfigured(t *testing.T) {
	client, _, _ := start(t)
	_, err := client.Test("dev", "long")
	if err == nil || !strings.Contains(err.Error(), "nothing configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestDevices(t *testing.T) {
	client, _, _ := start(t)
	devices, err := client.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != "dev" || devices[0].Name != "Bedside" || devices[0].Seq != 3 {
		t.Errorf("devices = %+v", devices)
	}
}

func TestForget(t *testing.T) {
	client, _, _ := start(t)
	if err := client.Forget("dev"); err != nil {
		t.Fatal(err)
	}
	devices, err := client.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Errorf("devices = %+v", devices)
	}
	if err := client.Forget("dev"); err == nil {
		t.Error("forgetting twice should fail")
	}
}

func TestPair(t *testing.T) {
	client, window, _ := start(t)
	result := make(chan string, 1)
	go func() {
		device, _ := client.Pair()
		result <- device
	}()
	for !window.IsOpen() {
		time.Sleep(time.Millisecond)
	}
	window.Claim("voistic-new")
	if device := <-result; device != "voistic-new" {
		t.Errorf("device = %q", device)
	}
}

func TestSocketIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no modes, the user's profile keeps the socket private")
	}
	_, _, path := start(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o", mode)
	}
}

func TestSecondServerRefuses(t *testing.T) {
	_, _, path := start(t)
	err := (&Server{}).Serve(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	dir, err := os.MkdirTemp("", "voistic")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "k.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleSocket(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat: %v", err)
	}
}

func TestAdoptGivesNewKey(t *testing.T) {
	client, _, socket := start(t)
	result, err := client.Adopt(AdoptRequest{Device: "dev", Board: "iot-button-c6", Firmware: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != "192.0.2.1:8787" || len(result.Key) != 64 {
		t.Errorf("result = %+v", result)
	}
	s, err := store.Open(filepath.Join(filepath.Dir(socket), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	device, ok := s.Device("dev")
	if !ok || device.Key != result.Key || device.Seq != 0 || device.Board != "iot-button-c6" {
		t.Errorf("stored = %+v", device)
	}
}

func TestAdoptWithdrawsWaitingFirmware(t *testing.T) {
	client, _, socket := start(t)
	store := firmware.Store{Dir: filepath.Join(filepath.Dir(socket), "firmware")}
	image := filepath.Join(filepath.Dir(socket), "image.bin")
	// An ESP32-C6 image header.
	if err := os.WriteFile(image, []byte("\xE9\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x0D\x00image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("dev", "iot-button-c6", image, "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Adopt(AdoptRequest{Device: "dev", Board: "iot-button-c6", Firmware: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("dev"); ok {
		t.Error("the firmware that waited before the pairing is still on offer")
	}
}

func TestAdoptNeedsDevice(t *testing.T) {
	client, _, _ := start(t)
	if _, err := client.Adopt(AdoptRequest{}); err == nil {
		t.Error("a request with no device should fail")
	}
}
