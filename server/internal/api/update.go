package api

import (
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// firmwareSendTime is how long a button gets to fetch an image.
const firmwareSendTime = 3 * time.Minute

type updateResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Size    int64  `json:"size,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Seq     uint64 `json:"seq"` // last, the firmware looks for it at the end
}

// FirmwareMessage is what a button signs to fetch its image.
func FirmwareMessage(device string, seq uint64) []byte {
	return fmt.Appendf(nil, "firmware|%s|%d", device, seq)
}

// update tells a button whether another image is waiting for it. A version that
// differs is enough, so going back to an older build works the same way. A button
// also calls this right after an update, and the answer is its proof of life.
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	req, device, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	switch {
	case req.Seq == device.Seq && len(device.Response) > 0:
		s.writeSigned(w, device.Key, device.Response)
		return
	case req.Seq <= device.Seq:
		s.Log.Warn("rejected stale update check", "device", req.Device, "seq", req.Seq, "last", device.Seq)
		http.Error(w, "stale seq", http.StatusConflict)
		return
	}

	answer := updateResponse{Status: StatusOK, Seq: req.Seq}
	if info, ok := s.Firmware.Get(req.Device); ok && info.Version != req.FW {
		answer.Status = StatusUpdate
		answer.Version = info.Version
		answer.Size = info.Size
		answer.SHA256 = info.SHA256
	}
	response, err := json.Marshal(answer)
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}

	device.Seq = req.Seq
	device.Response = response
	device.Firmware = req.FW
	device.LastSeen = time.Now()
	if err := s.Store.Put(req.Device, device); err != nil {
		s.Log.Error("can't save state", "device", req.Device, "err", err)
		http.Error(w, "can't save state", http.StatusInternalServerError)
		return
	}
	s.Log.Info("update check", "device", req.Device, "running", req.FW, "offered", answer.Version, "rssi", req.RSSI)
	s.writeSigned(w, device.Key, response)
}

// firmware sends a button its image. The signature only keeps strangers out: the
// hash in the signed answer to update is what the button checks.
func (s *Server) firmware(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	id := query.Get("device")
	seq, err := strconv.ParseUint(query.Get("seq"), 10, 64)
	if err != nil {
		http.Error(w, "bad seq", http.StatusBadRequest)
		return
	}
	device, ok := s.Store.Device(id)
	if !ok {
		http.Error(w, "unknown device", http.StatusUnauthorized)
		return
	}
	expected, err := Sign(device.Key, FirmwareMessage(id, seq))
	if err != nil {
		s.Log.Error("stored key is invalid", "device", id, "err", err)
		http.Error(w, "bad key", http.StatusInternalServerError)
		return
	}
	if !hmac.Equal([]byte(expected), []byte(r.Header.Get(SignatureHeader))) {
		s.Log.Warn("rejected a bad signature", "device", id)
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if seq != device.Seq {
		http.Error(w, "stale seq", http.StatusConflict)
		return
	}
	info, ok := s.Firmware.Get(id)
	if !ok {
		http.Error(w, "no firmware", http.StatusNotFound)
		return
	}
	image, err := s.Firmware.Open(id)
	if err != nil {
		s.Log.Error("can't open firmware", "device", id, "err", err)
		http.Error(w, "no firmware", http.StatusNotFound)
		return
	}
	defer image.Close()

	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(firmwareSendTime))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	start := time.Now()
	sent, err := io.Copy(w, image)
	if err != nil {
		s.Log.Warn("firmware download broke off", "device", id, "sent", sent, "err", err)
		return
	}
	s.Log.Info("firmware sent", "device", id, "version", info.Version, "bytes", sent, "took", time.Since(start).Round(time.Millisecond))
}
