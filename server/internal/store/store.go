// Package store keeps the machine-written state: device keys and press
// counters. It's separate from the config so the file with keys stays private
// and is never hand-edited.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Device is the state of one paired button.
type Device struct {
	Key      string          `json:"key"`
	Seq      uint64          `json:"seq"`
	Response json.RawMessage `json:"response,omitempty"` // answer to Seq, for retries
	Firmware string          `json:"firmware,omitempty"`
	Board    string          `json:"board,omitempty"`      // as reported, see board.Of
	Battery  int             `json:"battery_mv,omitempty"` // last reported, in mV
	PairedAt time.Time       `json:"paired_at"`
	LastSeen time.Time       `json:"last_seen,omitzero"`
}

// Store is a JSON file guarded by a mutex.
type Store struct {
	path string

	mu      sync.Mutex
	devices map[string]Device
}

// Open reads the store at path, starting empty if the file doesn't exist.
func Open(path string) (*Store, error) {
	s := &Store{path: path, devices: map[string]Device{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.devices); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Device returns the state of a device.
func (s *Store) Device(id string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.devices[id]
	return device, ok
}

// Devices returns a copy of all devices.
func (s *Store) Devices() map[string]Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.devices)
}

// Put saves a device. The in-memory state only changes if the write succeeds,
// so a failed disk write can't let a press counter roll back after a restart.
func (s *Store) Put(id string, device Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := maps.Clone(s.devices)
	next[id] = device
	if err := s.write(next); err != nil {
		return err
	}
	s.devices = next
	return nil
}

// Delete removes a device.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := maps.Clone(s.devices)
	delete(next, id)
	if err := s.write(next); err != nil {
		return err
	}
	s.devices = next
	return nil
}

func (s *Store) write(devices map[string]Device) error {
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// CreateTemp makes the file 0600, which the rename keeps.
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
