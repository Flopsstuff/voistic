package api

import (
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"voistic/server/internal/recording"
)

const (
	// An hour of 16 kHz audio, so a stuck button can't fill the disk.
	maxAudioBytes = 16000 * 2 * 3600
	minAudioRate  = 8000
	maxAudioRate  = 48000
	// The server's own read limit is per request, which a recording outlasts.
	// This one is per read, so a button that vanishes still gets noticed.
	audioIdle = 5 * time.Second
)

// AudioMessage is what a button signs to open a recording. There's no signed
// body as with a press, since the audio isn't known yet.
func AudioMessage(device string, seq uint64, rate int) []byte {
	return fmt.Appendf(nil, "audio|%s|%d|%d", device, seq, rate)
}

// audio saves the 16-bit mono PCM a button streams while it's held. If the
// stream breaks, what arrived is kept, since half a sentence beats none.
func (s *Server) audio(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	id := query.Get("device")
	seq, err := strconv.ParseUint(query.Get("seq"), 10, 64)
	if err != nil {
		http.Error(w, "bad seq", http.StatusBadRequest)
		return
	}
	rate, err := strconv.Atoi(query.Get("rate"))
	if err != nil || rate < minAudioRate || rate > maxAudioRate {
		http.Error(w, "bad rate", http.StatusBadRequest)
		return
	}

	device, ok := s.Store.Device(id)
	if !ok {
		http.Error(w, "unknown device", http.StatusUnauthorized)
		return
	}
	expected, err := Sign(device.Key, AudioMessage(id, seq, rate))
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
	if seq <= device.Seq {
		s.Log.Warn("rejected stale recording", "device", id, "seq", seq, "last", device.Seq)
		http.Error(w, "stale seq", http.StatusConflict)
		return
	}

	cfg, err := s.Config()
	if err != nil || cfg.Recordings == "" {
		s.Log.Error("no recordings folder in the config", "err", err)
		http.Error(w, "no recordings folder", http.StatusServiceUnavailable)
		return
	}

	// Saved first, so the same request can't be replayed once the stream ends.
	response, err := json.Marshal(pressResponse{Status: StatusOK, Awake: int(cfg.AwakeFor(id).Seconds()), Seq: seq})
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	device.Seq = seq
	device.Response = response
	device.LastSeen = time.Now()
	if err := s.Store.Put(id, device); err != nil {
		s.Log.Error("can't save state", "device", id, "err", err)
		http.Error(w, "can't save state", http.StatusInternalServerError)
		return
	}

	file, err := recording.Create(cfg.Recordings, id, rate, time.Now())
	if err != nil {
		s.Log.Error("can't start a recording", "device", id, "err", err)
		http.Error(w, "can't save", http.StatusInternalServerError)
		return
	}

	control := http.NewResponseController(w)
	body := http.MaxBytesReader(w, r.Body, maxAudioBytes)
	buffer := make([]byte, 8192)
	var streamErr error
	for {
		control.SetReadDeadline(time.Now().Add(audioIdle))
		n, err := body.Read(buffer)
		if n > 0 {
			if _, werr := file.Write(buffer[:n]); werr != nil {
				s.Log.Error("can't write a recording", "device", id, "err", werr)
				file.Discard()
				http.Error(w, "can't save", http.StatusInternalServerError)
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				streamErr = err
			}
			break
		}
	}

	path, length, err := file.Close()
	if err != nil {
		s.Log.Error("can't finish a recording", "device", id, "err", err)
		http.Error(w, "can't save", http.StatusInternalServerError)
		return
	}
	if s.Transcribe != nil {
		s.Transcribe(path)
	}
	if streamErr != nil {
		s.Log.Warn("recording cut short", "device", id, "path", path, "length", length.Round(time.Millisecond), "err", streamErr)
		return
	}
	s.Log.Info("recording saved", "device", id, "seq", seq, "path", path, "length", length.Round(time.Millisecond), "rssi", query.Get("rssi"))
	control.SetWriteDeadline(time.Now().Add(audioIdle))
	s.writeSigned(w, device.Key, response)
}
