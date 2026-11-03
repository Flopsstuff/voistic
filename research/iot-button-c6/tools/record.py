"""Talks to the mic test firmware over USB.

    python record.py stats            idle statistics, radio quiet and sending
    python record.py wifi <ssid>      save a network, password on stdin
    python record.py wait <dir> [n]   wait for n button recordings, save WAVs

Each recording is saved twice: `<time>-raw.wav` with a fixed gain, so levels
can be compared, and `<time>-norm.wav` normalised to -1 dBFS for listening.
"""

import array
import glob
import os
import sys
import time
import wave

import serial

BAUD = 115200


def open_port():
    ports = glob.glob("/dev/cu.usbmodem*")
    if not ports:
        sys.exit("no board on USB")
    return serial.Serial(ports[0], BAUD, timeout=1)


def read_line(port, deadline):
    while time.time() < deadline:
        line = port.readline()
        if line:
            return line.decode(errors="replace").rstrip()
    return None


def stats(port, command=b"s\n"):
    port.reset_input_buffer()
    port.write(command)
    deadline = time.time() + 5
    while (line := read_line(port, deadline)) is not None:
        print(line)
        if line.startswith("idle"):
            if command == b"S\n":
                print(read_line(port, deadline))  # packets and rssi
            return


def wifi(ssid):
    """Reads the password from stdin, so it never shows up anywhere."""
    password = sys.stdin.readline().rstrip("\n")
    if not password:
        sys.exit("no password on stdin")
    port = open_port()
    port.write(f"wifi {ssid}|{password}\n".encode())
    deadline = time.time() + 20
    while (line := read_line(port, deadline)) is not None:
        if line.startswith("wifi"):
            print(line)
            if "connected" in line:
                return
    sys.exit("did not connect within 20 s")


def fetch(port):
    port.write(b"d\n")
    deadline = time.time() + 10
    while (line := read_line(port, deadline)) is not None:
        if line.startswith("DATA "):
            count, rate, oversample = (int(x) for x in line.split()[1:4])
            raw = port.read(count * 2)
            if len(raw) != count * 2:
                sys.exit(f"short read: {len(raw)} of {count * 2} bytes")
            values = array.array("H")
            values.frombytes(raw)
            return list(values), rate, oversample
        print(line)
    sys.exit("no DATA from the board")


def high_pass(values):
    """Removes the DC bias with a one-pole filter, about 20 Hz at 16 kHz."""
    pole = 0.992
    mean = sum(values[:800]) / min(len(values), 800)
    last_in, last_out, out = mean, 0.0, []
    for v in values:
        y = v - last_in + pole * last_out
        last_in, last_out = v, y
        out.append(y)
    return out


def write_wav(path, samples, rate):
    data = array.array("h", (max(-32768, min(32767, int(round(s)))) for s in samples))
    with wave.open(path, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        w.writeframes(data.tobytes())


def save(directory, values, rate, oversample):
    stamp = time.strftime("%H%M%S")
    filtered = high_pass(values)
    # A sum of 4 12-bit values is 14 bits, so x4 brings it to 16 bits.
    fixed = [s * (65536 / (4096 * oversample)) for s in filtered]
    peak = max(1.0, max(abs(s) for s in filtered))
    norm = [s * (32767 * 0.89 / peak) for s in filtered]
    raw_path = os.path.join(directory, f"{stamp}-raw.wav")
    norm_path = os.path.join(directory, f"{stamp}-norm.wav")
    write_wav(raw_path, fixed, rate)
    write_wav(norm_path, norm, rate)
    peak_db = 20 * __import__("math").log10(max(abs(s) for s in fixed) / 32768 + 1e-9)
    print(f"saved {norm_path} ({len(values) / rate:.2f} s at {rate} Hz, raw peak {peak_db:.1f} dBFS)")
    return norm_path


def wait(directory, wanted):
    os.makedirs(directory, exist_ok=True)
    port = open_port()
    stats(port)
    got = 0
    deadline = time.time() + 600
    while got < wanted and time.time() < deadline:
        line = read_line(port, deadline)
        if line is None:
            break
        if line.startswith("idle"):
            continue
        print(line, flush=True)
        if line.startswith("REC "):
            values, rate, oversample = fetch(port)
            save(directory, values, rate, oversample)
            got += 1


def main():
    if len(sys.argv) >= 2 and sys.argv[1] == "stats":
        port = open_port()
        stats(port, b"s\n")
        stats(port, b"S\n")
    elif len(sys.argv) >= 3 and sys.argv[1] == "wifi":
        wifi(sys.argv[2])
    elif len(sys.argv) >= 3 and sys.argv[1] == "wait":
        wait(sys.argv[2], int(sys.argv[3]) if len(sys.argv) > 3 else 1)
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
