// Package control lets the voistic CLI talk to a running server over a Unix
// socket, so local commands need no network port or authentication beyond file
// permissions.
package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"voistic/server/internal/action"
	"voistic/server/internal/board"
	"voistic/server/internal/config"
	"voistic/server/internal/firmware"
	"voistic/server/internal/pairing"
	"voistic/server/internal/store"
)

// PairWindow is how long `voistic pair` waits for a button.
const PairWindow = 2 * time.Minute

// Device is a line of `voistic devices`.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Firmware string    `json:"firmware"`
	Board    string    `json:"board"`
	Battery  int       `json:"battery_mv"`
	Seq      uint64    `json:"seq"`
	LastSeen time.Time `json:"last_seen"`
}

// TestRequest asks to run a gesture's action.
type TestRequest struct {
	Device  string `json:"device"`
	Gesture string `json:"gesture"`
}

// TestResult is the outcome of an action run from the CLI.
type TestResult struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// PairResult names the device that paired.
type PairResult struct {
	Device string `json:"device"`
}

// AdoptRequest asks for a key for a button on this computer's USB, which then
// skips pairing over the network.
type AdoptRequest struct {
	Device   string `json:"device"`
	Board    string `json:"board"`
	Firmware string `json:"firmware"`
}

// AdoptResult is what the button needs to reach this server.
type AdoptResult struct {
	Key     string `json:"key"`
	Address string `json:"address"` // host:port
}

// Server answers the CLI.
type Server struct {
	Config  func() (*config.Config, error)
	Store   *store.Store
	Pairing *pairing.Window
	// Firmware is where images wait for buttons. A button paired over USB
	// starts afresh, so what waited for it before is withdrawn.
	Firmware firmware.Store
	Refresh  func() string // renews mDNS, returns where buttons find this server
	Log      *slog.Logger
}

// Serve listens on the socket until ctx ends.
func (s *Server) Serve(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := removeStaleSocket(path); err != nil {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	// Windows has no modes for a socket. The state folder in the user's profile
	// is what keeps it private there.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			listener.Close()
			return err
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /pair", s.pair)
	mux.HandleFunc("POST /test", s.test)
	mux.HandleFunc("POST /forget", s.forget)
	mux.HandleFunc("POST /adopt", s.adopt)
	mux.HandleFunc("GET /devices", s.devices)

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// removeStaleSocket clears a socket left by a crash, but refuses to take over
// from a server that's still running.
func removeStaleSocket(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return errors.New("another voistic server is already running")
	}
	return os.Remove(path)
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), PairWindow)
	defer cancel()

	s.Log.Info("pairing window open", "address", s.Refresh(), "for", PairWindow)
	device, err := s.Pairing.Wait(ctx)
	switch {
	case errors.Is(err, pairing.ErrBusy):
		s.Log.Warn("pairing window already open")
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, context.DeadlineExceeded):
		s.Log.Warn("pairing window closed, no button showed up")
		http.Error(w, "no button showed up", http.StatusGatewayTimeout)
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		writeJSON(w, PairResult{Device: device})
	}
}

func (s *Server) test(w http.ResponseWriter, r *http.Request) {
	var req TestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	cfg, err := s.Config()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	configured, ok := cfg.Action(req.Device, req.Gesture)
	if !ok {
		http.Error(w, fmt.Sprintf("nothing configured for %s %s", req.Device, req.Gesture), http.StatusNotFound)
		return
	}

	result := TestResult{}
	result.Output, err = action.Run(r.Context(), configured, action.Event{Device: req.Device, Gesture: req.Gesture})
	if err != nil {
		result.Error = err.Error()
	}
	writeJSON(w, result)
}

func (s *Server) forget(w http.ResponseWriter, r *http.Request) {
	var req TestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if _, ok := s.Store.Device(req.Device); !ok {
		http.Error(w, "no such device: "+req.Device, http.StatusNotFound)
		return
	}
	if err := s.Store.Delete(req.Device); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, struct{}{})
}

// adopt pairs a button handed over USB. Holding it is the proof that it belongs
// here, so no pairing window is needed.
func (s *Server) adopt(w http.ResponseWriter, r *http.Request) {
	var req AdoptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Device == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	address := s.Refresh()
	if address == "offline" {
		http.Error(w, "this computer has no network address for the button", http.StatusServiceUnavailable)
		return
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	key := hex.EncodeToString(raw)
	// A re-paired device counts from zero again, so it gets a new record.
	device := store.Device{Key: key, Firmware: req.Firmware, Board: req.Board, PairedAt: time.Now()}
	if err := s.Store.Put(req.Device, device); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.Firmware.Remove(req.Device); err != nil {
		s.Log.Error("can't withdraw the firmware waiting for a button", "device", req.Device, "err", err)
	}
	s.Log.Info("paired over USB", "device", req.Device, "board", board.Of(req.Board), "fw", req.Firmware, "address", address)
	writeJSON(w, AdoptResult{Key: key, Address: address})
}

func (s *Server) devices(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.Config()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	devices := []Device{}
	for id, device := range s.Store.Devices() {
		devices = append(devices, Device{
			ID:       id,
			Name:     cfg.Devices[id].Name,
			Firmware: device.Firmware,
			Board:    board.Of(device.Board),
			Battery:  device.Battery,
			Seq:      device.Seq,
			LastSeen: device.LastSeen,
		})
	}
	writeJSON(w, devices)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

// Client calls a running server.
type Client struct {
	http http.Client
}

// NewClient returns a Client for the socket at path.
func NewClient(path string) *Client {
	return &Client{http: http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", path)
		},
	}}}
}

// Pair waits for a button to pair and returns its id.
func (c *Client) Pair() (string, error) {
	var result PairResult
	err := c.call(http.MethodPost, "/pair", nil, &result)
	return result.Device, err
}

// Test runs a gesture's action.
func (c *Client) Test(device, gesture string) (TestResult, error) {
	var result TestResult
	err := c.call(http.MethodPost, "/test", TestRequest{Device: device, Gesture: gesture}, &result)
	return result, err
}

// Forget drops a device's key, so it can pair again.
func (c *Client) Forget(device string) error {
	var result struct{}
	return c.call(http.MethodPost, "/forget", TestRequest{Device: device}, &result)
}

// Adopt gives a button on USB a key, see AdoptRequest.
func (c *Client) Adopt(req AdoptRequest) (AdoptResult, error) {
	var result AdoptResult
	err := c.call(http.MethodPost, "/adopt", req, &result)
	return result, err
}

// Devices lists paired buttons.
func (c *Client) Devices() ([]Device, error) {
	var devices []Device
	err := c.call(http.MethodGet, "/devices", nil, &devices)
	return devices, err
}

func (c *Client) call(method, path string, request, response any) error {
	var body io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, "http://voistic"+path, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach the server, is `voistic serve` running? %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New(strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, response)
}
