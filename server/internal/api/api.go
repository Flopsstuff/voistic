// Package api serves the LAN protocol spoken by the buttons.
//
// Presses are authenticated with an HMAC of the body under the device's key,
// and the seq counter must grow, so a recorded press can't be replayed. A retry
// of the latest seq gets the stored answer without running the action again, so
// retries over a flaky link are safe. Responses echo seq and are signed too, so
// a button can't be fooled by a forged or replayed answer.
package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"time"

	"voistic/server/internal/config"
	"voistic/server/internal/firmware"
	"voistic/server/internal/pairing"
	"voistic/server/internal/store"
)

const (
	// SignatureHeader carries the hex HMAC-SHA256 of the body.
	SignatureHeader = "X-Voistic-Sig"

	maxBodyBytes = 4096
	keyBytes     = 32
)

// Press statuses.
const (
	StatusOK      = "ok"      // action started
	StatusUnbound = "unbound" // nothing configured for this gesture
	StatusError   = "error"   // the server couldn't tell what to do
	StatusUpdate  = "update"  // another firmware is waiting for this button
)

var deviceID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,32}$`)

type pressRequest struct {
	Device  string `json:"device"`
	Gesture string `json:"gesture"`
	Seq     uint64 `json:"seq"`
	FW      string `json:"fw"`
	RSSI    int    `json:"rssi"` // Wi-Fi signal at the button in dBm, for the log
}

type pressResponse struct {
	Status string `json:"status"`
	Seq    uint64 `json:"seq"`
}

type pairRequest struct {
	Device string `json:"device"`
	FW     string `json:"fw"`
}

type pairResponse struct {
	Key string `json:"key"`
}

// Server handles the button protocol.
type Server struct {
	Config   func() (*config.Config, error)
	Store    *store.Store
	Pairing  *pairing.Window
	Firmware firmware.Store
	// Dispatch starts an action. It must return without waiting for it, because
	// the button is waiting for the answer so it can go back to sleep.
	Dispatch func(device, gesture string, action config.Action)
	// Transcribe gets each finished recording. It must return without waiting,
	// for the same reason as Dispatch. May be nil.
	Transcribe func(path string)
	Log        *slog.Logger
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/press", s.press)
	mux.HandleFunc("POST /v1/audio", s.audio)
	mux.HandleFunc("POST /v1/update", s.update)
	mux.HandleFunc("GET /v1/firmware", s.firmware)
	mux.HandleFunc("POST /v1/pair", s.pair)
	mux.HandleFunc("POST /v1/unpair", s.unpair)
	return mux
}

// Sign returns the hex signature of body under a hex key.
func Sign(hexKey string, body []byte) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// authenticate reads a signed request and checks it against the device's key.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (pressRequest, store.Device, bool) {
	var req pressRequest
	body, ok := readBody(w, r)
	if !ok {
		return req, store.Device{}, false
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return req, store.Device{}, false
	}

	device, ok := s.Store.Device(req.Device)
	if !ok {
		http.Error(w, "unknown device", http.StatusUnauthorized)
		return req, store.Device{}, false
	}
	expected, err := Sign(device.Key, body)
	if err != nil {
		s.Log.Error("stored key is invalid", "device", req.Device, "err", err)
		http.Error(w, "bad key", http.StatusInternalServerError)
		return req, store.Device{}, false
	}
	if !hmac.Equal([]byte(expected), []byte(r.Header.Get(SignatureHeader))) {
		s.Log.Warn("rejected a bad signature", "device", req.Device)
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return req, store.Device{}, false
	}
	return req, device, true
}

func (s *Server) press(w http.ResponseWriter, r *http.Request) {
	req, device, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	switch {
	case req.Seq == device.Seq && len(device.Response) > 0:
		s.writeSigned(w, device.Key, device.Response)
		return
	case req.Seq <= device.Seq:
		s.Log.Warn("rejected stale press", "device", req.Device, "seq", req.Seq, "last", device.Seq)
		http.Error(w, "stale seq", http.StatusConflict)
		return
	case !slices.Contains(config.Gestures, req.Gesture):
		http.Error(w, "unknown gesture", http.StatusBadRequest)
		return
	}

	action, status := s.resolve(req)
	response, err := json.Marshal(pressResponse{Status: status, Seq: req.Seq})
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}

	// Saved before dispatching: a crash may then lose an action, but can't run
	// one twice.
	device.Seq = req.Seq
	device.Response = response
	device.Firmware = req.FW
	device.LastSeen = time.Now()
	if err := s.Store.Put(req.Device, device); err != nil {
		s.Log.Error("can't save state", "device", req.Device, "err", err)
		http.Error(w, "can't save state", http.StatusInternalServerError)
		return
	}

	s.Log.Info("press", "device", req.Device, "gesture", req.Gesture, "seq", req.Seq, "status", status, "rssi", req.RSSI)
	if status == StatusOK {
		s.Dispatch(req.Device, req.Gesture, action)
	}
	s.writeSigned(w, device.Key, response)
}

func (s *Server) resolve(req pressRequest) (config.Action, string) {
	cfg, err := s.Config()
	if err != nil {
		s.Log.Error("no usable config", "err", err)
		return config.Action{}, StatusError
	}
	action, ok := cfg.Action(req.Device, req.Gesture)
	if !ok {
		return config.Action{}, StatusUnbound
	}
	return action, StatusOK
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req pairRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if !deviceID.MatchString(req.Device) {
		http.Error(w, "bad device id", http.StatusBadRequest)
		return
	}
	// Only a `voistic pair` window lets a device in, so pairing is always asked for
	// on this computer. With several servers on a network, the others refuse.
	if !s.Pairing.Claim(req.Device) {
		s.Log.Warn("refused pairing", "device", req.Device)
		http.Error(w, "pairing is closed", http.StatusForbidden)
		return
	}

	raw := make([]byte, keyBytes)
	rand.Read(raw)
	key := hex.EncodeToString(raw)

	// A re-paired device starts counting from zero again, so it gets a new
	// record rather than an update.
	device := store.Device{Key: key, Firmware: req.FW, PairedAt: time.Now()}
	if err := s.Store.Put(req.Device, device); err != nil {
		s.Log.Error("can't save state", "device", req.Device, "err", err)
		http.Error(w, "can't save state", http.StatusInternalServerError)
		return
	}
	s.Log.Info("paired", "device", req.Device)
	writeJSON(w, pairResponse{Key: key})
}

// unpair lets a button give up its key before a factory reset, so it can pair
// again afterwards. The seq check stops a recorded request from being replayed.
func (s *Server) unpair(w http.ResponseWriter, r *http.Request) {
	req, device, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if req.Seq <= device.Seq {
		http.Error(w, "stale seq", http.StatusConflict)
		return
	}
	if err := s.Store.Delete(req.Device); err != nil {
		s.Log.Error("can't save state", "device", req.Device, "err", err)
		http.Error(w, "can't save state", http.StatusInternalServerError)
		return
	}
	s.Log.Info("unpaired", "device", req.Device)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) writeSigned(w http.ResponseWriter, key string, body []byte) {
	signature, err := Sign(key, body)
	if err != nil {
		http.Error(w, "bad key", http.StatusInternalServerError)
		return
	}
	w.Header().Set(SignatureHeader, signature)
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "can't read body", http.StatusBadRequest)
		}
		return nil, false
	}
	return body, true
}
