// Package usb talks to a button plugged into this computer, over the serial
// line its firmware reads commands from. See handleCommand in src/main.cpp for
// the other side.
package usb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.bug.st/serial"
)

const (
	pollInterval  = 200 * time.Millisecond
	helloInterval = 500 * time.Millisecond
	// The firmware takes commands for 10 s after a restart, then sleeps.
	helloTimeout = 8 * time.Second
	resetPulse   = 200 * time.Millisecond
	// A restarted board drops off USB for a moment and comes back.
	reappearTimeout = 10 * time.Second
	// Commands answer at once, apart from those that wait for Wi-Fi.
	answerTimeout = 3 * time.Second
)

// ErrGone means the board left USB, by sleeping or being unplugged.
var ErrGone = errors.New("the board went off USB")

// Status is what a button says about itself.
type Status struct {
	Device     string  `json:"device"`
	Board      string  `json:"board"`
	Firmware   string  `json:"fw"`
	Paired     bool    `json:"paired"`
	Server     string  `json:"server"` // host:port it last reached
	SSID       string  `json:"ssid"`   // the saved network
	Connected  bool    `json:"connected"`
	IP         string  `json:"ip"`
	RSSI       int     `json:"rssi"`
	Mic        bool    `json:"mic"`
	Battery    int     `json:"battery_mv"` // 0 where the board can't measure it
	Brightness float64 `json:"brightness"`
}

// Board is a button in a USB session: it stays awake and ignores its button
// until End.
type Board struct {
	Port string
	// Log gets every line the board prints that isn't an answer. May be nil.
	Log func(string)

	port  serial.Port
	lines chan string
}

// WaitPort returns a button's port, waiting until one appears.
func WaitPort(ctx context.Context) (string, error) {
	for {
		ports, err := Ports()
		if err != nil {
			return "", err
		}
		if len(ports) > 0 {
			return ports[0], nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// open leaves DTR and RTS alone, since together they restart the chip.
func open(name string) (serial.Port, error) {
	return serial.Open(name, &serial.Mode{BaudRate: 115200, InitialStatusBits: &serial.ModemOutputBits{}})
}

// Connect finds a button, restarts it, so it's awake whatever it was doing, and
// starts a session.
func Connect(ctx context.Context, log func(string)) (*Board, Status, error) {
	name, err := WaitPort(ctx)
	if err != nil {
		return nil, Status{}, err
	}
	if err := restart(name); err != nil {
		return nil, Status{}, fmt.Errorf("can't restart the board on %s: %w", name, err)
	}

	ctx, cancel := context.WithTimeout(ctx, reappearTimeout)
	defer cancel()
	waitGone(name)
	var port serial.Port
	for port == nil {
		if name, err = WaitPort(ctx); err != nil {
			return nil, Status{}, errors.New("the board didn't come back after the restart")
		}
		if port, err = open(name); err != nil {
			time.Sleep(pollInterval) // still settling
		}
	}

	b := &Board{Port: name, Log: log, port: port, lines: make(chan string, 256)}
	go b.read()
	status, err := b.hello(ctx)
	if err != nil {
		b.port.Close()
		return nil, Status{}, err
	}
	return b, status, nil
}

// restart pulses RTS with DTR low, which the ESP32's USB bridge takes as a reset
// into the firmware, as esptool's hard reset does.
func restart(name string) error {
	port, err := open(name)
	if err != nil {
		return err
	}
	defer port.Close()
	if err := port.SetDTR(false); err != nil {
		return err
	}
	if err := port.SetRTS(true); err != nil {
		return err
	}
	time.Sleep(resetPulse)
	port.SetRTS(false) // the board may already be gone
	time.Sleep(resetPulse)
	return nil
}

// waitGone gives a restarting board a moment to drop off USB, so the old port
// isn't opened again just before it goes.
func waitGone(name string) {
	for deadline := time.Now().Add(1500 * time.Millisecond); time.Now().Before(deadline); {
		ports, _ := Ports()
		if !slices.Contains(ports, name) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (b *Board) read() {
	scanner := bufio.NewScanner(b.port)
	for scanner.Scan() {
		b.lines <- strings.TrimRight(scanner.Text(), "\r")
	}
	close(b.lines)
}

func (b *Board) log(line string) {
	if b.Log != nil && line != "" {
		b.Log(line)
	}
}

// hello repeats itself, since the board only listens once it has booted.
func (b *Board) hello(ctx context.Context) (Status, error) {
	timeout := time.After(helloTimeout)
	ticker := time.NewTicker(helloInterval)
	defer ticker.Stop()
	if err := b.write("hello"); err != nil {
		return Status{}, err
	}
	for {
		select {
		case line, ok := <-b.lines:
			if !ok {
				return Status{}, ErrGone
			}
			if rest, found := strings.CutPrefix(line, "hello: "); found {
				return parseStatus(rest)
			}
			b.log(line)
		case <-ticker.C:
			if err := b.write("hello"); err != nil {
				return Status{}, err
			}
		case <-timeout:
			return Status{}, errors.New("the board doesn't answer, is its firmware older than voistic usb?")
		case <-ctx.Done():
			return Status{}, ctx.Err()
		}
	}
}

func (b *Board) write(line string) error {
	_, err := b.port.Write([]byte(line + "\n"))
	return err
}

// Command sends a line and returns the board's answer to it, the text after
// "name: ". An "error: " line comes back as an error.
func (b *Board) Command(ctx context.Context, line string, timeout time.Duration) (string, error) {
	name, _, _ := strings.Cut(line, " ")
	if err := b.write(line); err != nil {
		return "", err
	}
	expired := time.After(timeout)
	for {
		select {
		case got, ok := <-b.lines:
			if !ok {
				return "", ErrGone
			}
			if rest, found := strings.CutPrefix(got, name+": "); found {
				return rest, nil
			}
			if rest, found := strings.CutPrefix(got, "error: "); found {
				return "", errors.New(rest)
			}
			b.log(got)
		case <-expired:
			return "", fmt.Errorf("no answer from the board to %s", name)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// Status asks the board how it is.
func (b *Board) Status(ctx context.Context) (Status, error) {
	answer, err := b.Command(ctx, "status", answerTimeout)
	if err != nil {
		return Status{}, err
	}
	return parseStatus(answer)
}

// SetWiFi saves a network on the board, which starts joining it.
func (b *Board) SetWiFi(ctx context.Context, ssid, password string) error {
	if strings.ContainsAny(ssid, "|\n") || strings.Contains(password, "\n") {
		return errors.New("the network name can't hold | or a line break")
	}
	_, err := b.Command(ctx, "wifi "+ssid+"|"+password, answerTimeout)
	return err
}

// WaitConnected polls the board until it's on its network or the time is up.
func (b *Board) WaitConnected(ctx context.Context, limit time.Duration) (Status, error) {
	deadline := time.Now().Add(limit)
	for {
		status, err := b.Status(ctx)
		if err != nil || status.Connected || time.Now().After(deadline) {
			return status, err
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// SetBrightness sets the pixel, from 0 to 1.
func (b *Board) SetBrightness(ctx context.Context, brightness float64) error {
	_, err := b.Command(ctx, fmt.Sprintf("brightness %.2f", brightness), answerTimeout)
	return err
}

// Adopt hands the board a key and where its server is.
func (b *Board) Adopt(ctx context.Context, key, host, port string) error {
	_, err := b.Command(ctx, "pair "+key+" "+host+" "+port, answerTimeout)
	return err
}

// Check makes the board ask its server, joining Wi-Fi first if needed. It
// answers "ok", "update waiting" or what went wrong.
func (b *Board) Check(ctx context.Context) (string, error) {
	return b.Command(ctx, "check", 25*time.Second)
}

// Update makes the board take the firmware the server holds for it. It answers
// once the image is in, then restarts into it.
func (b *Board) Update(ctx context.Context) (string, error) {
	return b.Command(ctx, "update", 3*time.Minute)
}

// Reset makes the board forget its network and its server.
func (b *Board) Reset(ctx context.Context) error {
	_, err := b.Command(ctx, "reset", 25*time.Second)
	return err
}

// Close lets go of the port without a word to the board, for one that has
// restarted and left the session anyway.
func (b *Board) Close() error { return b.port.Close() }

// End ends the session, and closes the port. The board then carries on as
// after a press: ready for a while, then asleep.
func (b *Board) End(ctx context.Context) error {
	_, err := b.Command(ctx, "bye", answerTimeout)
	b.port.Close()
	if errors.Is(err, ErrGone) {
		return nil // asleep already
	}
	return err
}

func parseStatus(text string) (Status, error) {
	var status Status
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		return Status{}, fmt.Errorf("can't read the board's status %q: %w", text, err)
	}
	return status, nil
}

// Follow prints everything the board says, without restarting it, until ctx
// ends. A board that sleeps leaves USB, so it waits for it to come back, and
// gone is told each time.
func Follow(ctx context.Context, line func(string), gone func()) error {
	for {
		name, err := WaitPort(ctx)
		if err != nil {
			return nil // ctx ended
		}
		port, err := open(name)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		stop := context.AfterFunc(ctx, func() { port.Close() })
		scanner := bufio.NewScanner(port)
		for scanner.Scan() {
			line(strings.TrimRight(scanner.Text(), "\r"))
		}
		stop()
		port.Close()
		if ctx.Err() != nil {
			return nil
		}
		gone()
	}
}
