package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"voistic/server/internal/board"
	"voistic/server/internal/config"
	"voistic/server/internal/firmware"
	"voistic/server/internal/pairing"
	"voistic/server/internal/store"
)

const (
	testDevice = "voistic-8cbc"
	testKey    = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

type harness struct {
	api     *Server
	server  *httptest.Server
	store   *store.Store
	window  *pairing.Window
	mu      sync.Mutex
	actions []string
	config  func() (*config.Config, error)
}

func (h *harness) ran() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.actions...)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(testDevice, store.Device{Key: testKey}); err != nil {
		t.Fatal(err)
	}

	h := &harness{store: s, window: &pairing.Window{}}
	h.config = func() (*config.Config, error) {
		return &config.Config{Devices: map[string]config.Device{
			testDevice: {Gestures: map[string]config.Action{"single": {Exec: []string{"true"}}}},
		}}, nil
	}
	h.api = &Server{
		Config:   func() (*config.Config, error) { return h.config() },
		Store:    s,
		Pairing:  h.window,
		Firmware: firmware.Store{Dir: t.TempDir()},
		Dispatch: func(device, gesture string, _ config.Action) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.actions = append(h.actions, device+"/"+gesture)
		},
		Log: slog.New(slog.DiscardHandler),
	}
	h.server = httptest.NewServer(h.api.Handler())
	t.Cleanup(h.server.Close)
	return h
}

func (h *harness) post(t *testing.T, path, body, signature string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if signature != "" {
		req.Header.Set(SignatureHeader, signature)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, out.String()
}

func (h *harness) press(t *testing.T, gesture string, seq int) (*http.Response, string) {
	t.Helper()
	data, _ := json.Marshal(pressRequest{Device: testDevice, Gesture: gesture, Seq: uint64(seq), FW: "1.0.0"})
	signature, err := Sign(testKey, data)
	if err != nil {
		t.Fatal(err)
	}
	return h.post(t, "/v1/press", string(data), signature)
}

func TestPressRunsActionAndSignsAnswer(t *testing.T) {
	h := newHarness(t)
	resp, body := h.press(t, "single", 1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}

	var answer pressResponse
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Status != StatusOK || answer.Seq != 1 {
		t.Errorf("answer = %+v", answer)
	}
	if want, _ := Sign(testKey, []byte(body)); resp.Header.Get(SignatureHeader) != want {
		t.Error("answer signature is wrong")
	}
	if ran := h.ran(); len(ran) != 1 || ran[0] != testDevice+"/single" {
		t.Errorf("ran = %v", ran)
	}
}

func TestRetryDoesNotRunTwice(t *testing.T) {
	h := newHarness(t)
	_, first := h.press(t, "single", 1)
	resp, second := h.press(t, "single", 1)
	if resp.StatusCode != http.StatusOK || first != second {
		t.Errorf("status %d, first %q, second %q", resp.StatusCode, first, second)
	}
	if ran := h.ran(); len(ran) != 1 {
		t.Errorf("ran = %v", ran)
	}
}

func TestAnswerIsStoredForRetries(t *testing.T) {
	h := newHarness(t)
	h.press(t, "single", 5)
	device, _ := h.store.Device(testDevice)
	if device.Seq != 5 || len(device.Response) == 0 {
		t.Errorf("stored device = %+v", device)
	}
}

func TestStaleSeqRejected(t *testing.T) {
	h := newHarness(t)
	h.press(t, "single", 5)
	if resp, _ := h.press(t, "single", 4); resp.StatusCode != http.StatusConflict {
		t.Errorf("status %d", resp.StatusCode)
	}
	if ran := h.ran(); len(ran) != 1 {
		t.Errorf("ran = %v", ran)
	}
}

func TestSeqZeroRejectedOnFreshDevice(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.press(t, "single", 0); resp.StatusCode != http.StatusConflict {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestBadSignatureRejected(t *testing.T) {
	h := newHarness(t)
	data, _ := json.Marshal(pressRequest{Device: testDevice, Gesture: "single", Seq: 1})
	wrong, _ := Sign(strings.Repeat("ff", keyBytes), data)

	for name, signature := range map[string]string{"wrong key": wrong, "missing": ""} {
		t.Run(name, func(t *testing.T) {
			if resp, _ := h.post(t, "/v1/press", string(data), signature); resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status %d", resp.StatusCode)
			}
		})
	}
	if ran := h.ran(); len(ran) != 0 {
		t.Errorf("ran = %v", ran)
	}
	if device, _ := h.store.Device(testDevice); device.Seq != 0 {
		t.Errorf("Seq = %d, a forged press must not move it", device.Seq)
	}
}

func TestTamperedBodyRejected(t *testing.T) {
	h := newHarness(t)
	data, _ := json.Marshal(pressRequest{Device: testDevice, Gesture: "single", Seq: 1})
	signature, _ := Sign(testKey, data)
	tampered := strings.Replace(string(data), "single", "double", 1)
	if resp, _ := h.post(t, "/v1/press", tampered, signature); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestUnknownDeviceRejected(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.post(t, "/v1/press", `{"device":"other","gesture":"single","seq":1}`, "00"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestUnboundGesture(t *testing.T) {
	h := newHarness(t)
	_, body := h.press(t, "double", 1)
	var answer pressResponse
	json.Unmarshal([]byte(body), &answer)
	if answer.Status != StatusUnbound {
		t.Errorf("answer = %+v", answer)
	}
	if ran := h.ran(); len(ran) != 0 {
		t.Errorf("ran = %v", ran)
	}
}

func TestUnknownGestureRejected(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.press(t, "quad", 1); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestBodyTooLarge(t *testing.T) {
	h := newHarness(t)
	big := strings.Repeat("x", maxBodyBytes+1)
	if resp, _ := h.post(t, "/v1/press", big, ""); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func openWindow(t *testing.T, h *harness) <-chan string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	paired := make(chan string, 1)
	go func() {
		device, _ := h.window.Wait(ctx)
		paired <- device
	}()
	for !h.window.IsOpen() {
		time.Sleep(time.Millisecond)
	}
	return paired
}

func TestPairClosedByDefault(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.post(t, "/v1/pair", `{"device":"voistic-new"}`, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d", resp.StatusCode)
	}
	if _, ok := h.store.Device("voistic-new"); ok {
		t.Error("device stored while the window was closed")
	}
}

func TestPairGivesWorkingKey(t *testing.T) {
	h := newHarness(t)
	paired := openWindow(t, h)

	resp, body := h.post(t, "/v1/pair", `{"device":"voistic-new","fw":"1.0.0"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if device := <-paired; device != "voistic-new" {
		t.Errorf("paired %q", device)
	}

	var answer pairResponse
	json.Unmarshal([]byte(body), &answer)
	if len(answer.Key) != keyBytes*2 {
		t.Fatalf("key = %q", answer.Key)
	}

	data, _ := json.Marshal(pressRequest{Device: "voistic-new", Gesture: "single", Seq: 1})
	signature, _ := Sign(answer.Key, data)
	if resp, body := h.post(t, "/v1/press", string(data), signature); resp.StatusCode != http.StatusOK {
		t.Errorf("press after pairing: status %d: %s", resp.StatusCode, body)
	}

	if resp, _ := h.post(t, "/v1/pair", `{"device":"voistic-other"}`, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("second device: status %d", resp.StatusCode)
	}
}

func TestRepairResetsSeqAndOldKey(t *testing.T) {
	h := newHarness(t)
	h.press(t, "single", 9)

	openWindow(t, h)
	_, body := h.post(t, "/v1/pair", `{"device":"`+testDevice+`"}`, "")
	var answer pairResponse
	json.Unmarshal([]byte(body), &answer)

	data, _ := json.Marshal(pressRequest{Device: testDevice, Gesture: "single", Seq: 1})
	signature, _ := Sign(answer.Key, data)
	if resp, _ := h.post(t, "/v1/press", string(data), signature); resp.StatusCode != http.StatusOK {
		t.Errorf("new key: status %d", resp.StatusCode)
	}
	if resp, _ := h.press(t, "single", 10); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old key: status %d", resp.StatusCode)
	}
}

func unpairBody(t *testing.T, device string, seq int) (string, string) {
	t.Helper()
	data, _ := json.Marshal(pressRequest{Device: device, Seq: uint64(seq)})
	signature, err := Sign(testKey, data)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), signature
}

func TestConfiguredDeviceStillNeedsTheWindow(t *testing.T) {
	h := newHarness(t)
	body, signature := unpairBody(t, testDevice, 1)
	if resp, _ := h.post(t, "/v1/unpair", body, signature); resp.StatusCode != http.StatusOK {
		t.Fatalf("unpair: status %d", resp.StatusCode)
	}

	if resp, _ := h.post(t, "/v1/pair", `{"device":"`+testDevice+`"}`, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("pair without a window: status %d", resp.StatusCode)
	}
	paired := openWindow(t, h)
	if resp, body := h.post(t, "/v1/pair", `{"device":"`+testDevice+`"}`, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("pair: status %d: %s", resp.StatusCode, body)
	}
	<-paired
}

func TestPairedDeviceCannotBeTakenOver(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.post(t, "/v1/pair", `{"device":"`+testDevice+`"}`, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d", resp.StatusCode)
	}
	if device, _ := h.store.Device(testDevice); device.Key != testKey {
		t.Error("key changed")
	}
}

func TestUnknownDeviceNeedsTheWindow(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.post(t, "/v1/pair", `{"device":"voistic-stranger"}`, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestUnpairRejectsForgeryAndReplay(t *testing.T) {
	h := newHarness(t)
	h.press(t, "single", 5)

	body, signature := unpairBody(t, testDevice, 4)
	if resp, _ := h.post(t, "/v1/unpair", body, signature); resp.StatusCode != http.StatusConflict {
		t.Errorf("replay: status %d", resp.StatusCode)
	}
	body, _ = unpairBody(t, testDevice, 6)
	if resp, _ := h.post(t, "/v1/unpair", body, "deadbeef"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("forged: status %d", resp.StatusCode)
	}
	if _, ok := h.store.Device(testDevice); !ok {
		t.Error("device was removed")
	}
}

func TestPairRejectsBadDeviceID(t *testing.T) {
	h := newHarness(t)
	openWindow(t, h)
	t.Cleanup(func() { h.window.Claim("cleanup") })
	if resp, _ := h.post(t, "/v1/pair", `{"device":"../evil"}`, ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestAudioIsSavedAsAWavAndCantBeReplayed(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	h.config = func() (*config.Config, error) { return &config.Config{Recordings: dir}, nil }
	var transcribed []string
	h.api.Transcribe = func(path string) { transcribed = append(transcribed, path) }

	signature, err := Sign(testKey, AudioMessage(testDevice, 1, 16000))
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/audio?device=%s&seq=1&rate=16000", testDevice)
	resp, _ := h.post(t, path, strings.Repeat("ab", 16000), signature)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if resp.Header.Get(SignatureHeader) == "" {
		t.Error("the answer isn't signed")
	}
	saved, err := filepath.Glob(filepath.Join(dir, "*.wav"))
	if err != nil || len(saved) != 1 {
		t.Fatalf("saved %v, %v", saved, err)
	}
	info, err := os.Stat(saved[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 44+32000 {
		t.Errorf("size %d", info.Size())
	}
	if len(transcribed) != 1 || transcribed[0] != saved[0] {
		t.Errorf("transcribed %v", transcribed)
	}

	resp, _ = h.post(t, path, "abcd", signature)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("replay got %d", resp.StatusCode)
	}
}

func TestAudioNeedsAValidSignature(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	h.config = func() (*config.Config, error) { return &config.Config{Recordings: dir}, nil }

	path := fmt.Sprintf("/v1/audio?device=%s&seq=1&rate=16000", testDevice)
	resp, _ := h.post(t, path, "abcd", strings.Repeat("0", 64))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d", resp.StatusCode)
	}
	if saved, _ := filepath.Glob(filepath.Join(dir, "*")); len(saved) != 0 {
		t.Errorf("saved %v", saved)
	}
}

func TestUpdateOffersAnotherVersionAndServesIt(t *testing.T) {
	h := newHarness(t)
	source := filepath.Join(t.TempDir(), "firmware.bin")
	// An ESP32-S3 image header, the chip the test device's board has.
	image := "\xE9\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x09\x00image"
	if err := os.WriteFile(source, []byte(image), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := h.api.Firmware.Add(testDevice, board.XiaoS3, source, "new")
	if err != nil {
		t.Fatal(err)
	}

	check := func(seq uint64, version string) string {
		t.Helper()
		body := fmt.Sprintf(`{"device":%q,"seq":%d,"fw":%q}`, testDevice, seq, version)
		signature, err := Sign(testKey, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, answer := h.post(t, "/v1/update", body, signature)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
		return answer
	}

	if answer := check(1, "new"); !strings.Contains(answer, `"status":"ok"`) {
		t.Errorf("up to date got %s", answer)
	}
	answer := check(2, "old")
	if !strings.Contains(answer, `"status":"update"`) || !strings.Contains(answer, info.SHA256) ||
		!strings.HasSuffix(answer, `"seq":2}`) {
		t.Errorf("old got %s", answer)
	}

	fetch := func(seq uint64, signed bool) (int, string) {
		t.Helper()
		url := fmt.Sprintf("%s/v1/firmware?device=%s&seq=%d", h.server.URL, testDevice, seq)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if signed {
			signature, err := Sign(testKey, FirmwareMessage(testDevice, seq))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(SignatureHeader, signature)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	if code, data := fetch(2, true); code != http.StatusOK || data != image {
		t.Errorf("download got %d %q", code, data)
	}
	if code, _ := fetch(2, false); code != http.StatusUnauthorized {
		t.Errorf("unsigned download got %d", code)
	}
	if code, _ := fetch(1, true); code != http.StatusConflict {
		t.Errorf("old seq got %d", code)
	}
}

func TestPairAndPressRememberTheBoard(t *testing.T) {
	h := newHarness(t)
	openWindow(t, h)
	resp, body := h.post(t, "/v1/pair", `{"device":"voistic-c6","fw":"1.0.0","board":"iot-button-c6"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var answer pairResponse
	json.Unmarshal([]byte(body), &answer)
	if device, _ := h.store.Device("voistic-c6"); device.Board != "iot-button-c6" {
		t.Fatalf("board after pairing %q", device.Board)
	}

	data, _ := json.Marshal(pressRequest{Device: "voistic-c6", Gesture: "single", Seq: 1, Battery: 3912})
	signature, _ := Sign(answer.Key, data)
	if resp, body := h.post(t, "/v1/press", string(data), signature); resp.StatusCode != http.StatusOK {
		t.Fatalf("press: status %d: %s", resp.StatusCode, body)
	}
	device, _ := h.store.Device("voistic-c6")
	if device.Board != "iot-button-c6" || device.Battery != 3912 {
		t.Errorf("a press without a board forgot it, or lost the battery: %+v", device)
	}
}

func TestAnswerSaysHowLongToStayAwake(t *testing.T) {
	h := newHarness(t)
	_, body := h.press(t, "single", 1)
	var answer pressResponse
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Awake != int(config.DefaultDeepSleepAfter.Seconds()) {
		t.Errorf("awake_s = %d", answer.Awake)
	}
	if !strings.HasSuffix(body, fmt.Sprintf(`"seq":%d}`, answer.Seq)) {
		t.Errorf("seq isn't last, the firmware looks for it there: %s", body)
	}
}

func TestUpdateWontOfferAnImageForAnotherBoard(t *testing.T) {
	h := newHarness(t)
	source := filepath.Join(t.TempDir(), "firmware.bin")
	image := "\xE9\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x09\x00image"
	if err := os.WriteFile(source, []byte(image), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.api.Firmware.Add(testDevice, board.XiaoS3, source, "new"); err != nil {
		t.Fatal(err)
	}
	// Paired again since, as the other board.
	device, _ := h.store.Device(testDevice)
	device.Board = board.IoTButtonC6
	h.store.Put(testDevice, device)

	body := fmt.Sprintf(`{"device":%q,"seq":1,"fw":"old"}`, testDevice)
	signature, _ := Sign(testKey, []byte(body))
	if _, answer := h.post(t, "/v1/update", body, signature); !strings.Contains(answer, `"status":"ok"`) {
		t.Errorf("offered an S3 image to a C6: %s", answer)
	}
}
