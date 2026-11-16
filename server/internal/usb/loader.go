package usb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

// A chip that restarts while its battery collapses reads its BOOT pin as held,
// and starts the ROM's serial loader instead of the firmware. It waits there
// for an image forever, awake and deaf to its button, and charging it doesn't
// help: the pin is only read again by a reset from the power, the reset button
// or the RTC watchdog, not by one over USB. So a board that answers as the
// loader gets the watchdog, set through the loader itself, as esptool does.
//
// Only the ESP32-S3 so far: on the ESP32-C6 a watchdog reset upsets its USB,
// and esptool doesn't use it there either.

const (
	loaderSync     = 0x08
	loaderWriteReg = 0x09
	loaderReadReg  = 0x0A

	// The loader answers a sync at once, so a firmware costs this much.
	loaderSyncWait = 300 * time.Millisecond
	loaderWait     = 500 * time.Millisecond

	chipMagicReg = 0x40001000
	chipMagicS3  = 0x00000009

	// The ESP32-S3's RTC watchdog, see esptool's esp32s3.py.
	s3WdtConfig0 = 0x60008098
	s3WdtConfig1 = 0x6000809C
	s3WdtProtect = 0x600080B0
	wdtKey       = 0x50D83AA1
	wdtTimeout   = 2000                     // slow clock cycles, a moment
	wdtEnable    = 1<<31 | 5<<28 | 1<<8 | 2 // on, its first stage resets the chip
)

// ErrInLoader means the chip runs its ROM loader, and nothing here got it out.
var ErrInLoader = errors.New("the board is in its ROM loader, not its firmware: press its reset button, or flash it")

// wire is as much of a serial port as the loader needs. A read returns nothing
// when its time is up.
type wire interface {
	io.ReadWriter
	SetReadTimeout(time.Duration) error
}

type loader struct {
	port   wire
	frames [][]byte // answers read and not asked for yet
	frame  []byte
	open   bool // inside a frame
	esc    bool
}

// slip frames a packet as the loader reads it.
func slip(packet []byte) []byte {
	out := []byte{0xC0}
	for _, b := range packet {
		switch b {
		case 0xC0:
			out = append(out, 0xDB, 0xDC)
		case 0xDB:
			out = append(out, 0xDB, 0xDD)
		default:
			out = append(out, b)
		}
	}
	return append(out, 0xC0)
}

func (l *loader) send(op byte, data []byte) error {
	packet := []byte{0x00, op, byte(len(data)), byte(len(data) >> 8), 0, 0, 0, 0}
	_, err := l.port.Write(slip(append(packet, data...)))
	return err
}

// feed takes what the port said apart into frames. Anything between them,
// such as a firmware's log, is dropped.
func (l *loader) feed(data []byte) {
	for _, b := range data {
		switch {
		case b == 0xC0 && l.open && len(l.frame) > 0:
			l.frames = append(l.frames, l.frame)
			l.frame, l.open = nil, false
		case b == 0xC0:
			l.frame, l.open = nil, true
		case !l.open:
		case l.esc && b == 0xDC:
			l.frame, l.esc = append(l.frame, 0xC0), false
		case l.esc && b == 0xDD:
			l.frame, l.esc = append(l.frame, 0xDB), false
		case b == 0xDB:
			l.esc = true
		default:
			l.frame = append(l.frame, b)
		}
	}
}

// answer waits for the loader's answer to op and returns its value.
func (l *loader) answer(op byte, wait time.Duration) (uint32, bool) {
	l.port.SetReadTimeout(50 * time.Millisecond)
	buf := make([]byte, 1024)
	deadline := time.Now().Add(wait)
	for {
		for len(l.frames) > 0 {
			frame := l.frames[0]
			l.frames = l.frames[1:]
			if len(frame) >= 8 && frame[0] == 0x01 && frame[1] == op {
				return binary.LittleEndian.Uint32(frame[4:8]), true
			}
		}
		if !time.Now().Before(deadline) {
			return 0, false
		}
		n, err := l.port.Read(buf)
		if err != nil {
			return 0, false // the port is gone
		}
		l.feed(buf[:n])
	}
}

// sync tells whether the ROM loader is what's listening.
func (l *loader) sync() bool {
	data := append([]byte{0x07, 0x07, 0x12, 0x20}, bytes.Repeat([]byte{0x55}, 32)...)
	if l.send(loaderSync, data) != nil {
		return false
	}
	if _, ok := l.answer(loaderSync, loaderSyncWait); !ok {
		return false
	}
	// It answers a sync eight times over, and the rest would be in the way.
	for {
		if _, more := l.answer(loaderSync, 100*time.Millisecond); !more {
			return true
		}
	}
}

func (l *loader) readReg(addr uint32) (uint32, bool) {
	data := binary.LittleEndian.AppendUint32(nil, addr)
	if l.send(loaderReadReg, data) != nil {
		return 0, false
	}
	return l.answer(loaderReadReg, loaderWait)
}

func (l *loader) writeReg(addr, value uint32) bool {
	data := binary.LittleEndian.AppendUint32(nil, addr)
	data = binary.LittleEndian.AppendUint32(data, value)
	data = binary.LittleEndian.AppendUint32(data, 0xFFFFFFFF) // every bit
	data = binary.LittleEndian.AppendUint32(data, 0)          // no delay
	if l.send(loaderWriteReg, data) != nil {
		return false
	}
	_, ok := l.answer(loaderWriteReg, loaderWait)
	return ok
}

// leave gets a chip out of its ROM loader and into its firmware. It tells
// whether the loader was there, and whether the chip was restarted out of it.
func (l *loader) leave() (found, left bool) {
	if !l.sync() {
		return false, false
	}
	if magic, ok := l.readReg(chipMagicReg); !ok || magic != chipMagicS3 {
		return true, false
	}
	if !l.writeReg(s3WdtProtect, wdtKey) || !l.writeReg(s3WdtConfig1, wdtTimeout) {
		return true, false
	}
	// The chip restarts within a moment of this, so its answer and the lock
	// after it may never arrive.
	l.writeReg(s3WdtConfig0, wdtEnable)
	l.writeReg(s3WdtProtect, 0)
	return true, true
}

// leaveLoader does that for the board on a port. A firmware takes the sync for
// a line of noise, so it's only for a board that's about to be restarted, or
// that doesn't answer.
func leaveLoader(name string) (found, left bool, err error) {
	port, err := open(name)
	if err != nil {
		return false, false, err
	}
	defer port.Close()
	found, left = (&loader{port: port}).leave()
	return found, left, nil
}
