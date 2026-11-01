#!/usr/bin/env python3
"""Sends one setup command to the board over USB and shows its answer.

The port only exists while the board is awake, so hold the button. The board
takes commands for 10 s after a reset. Needs pyserial and PlatformIO's esptool.

    python tools/board.py "brightness 0.2"
"""

import glob
import os
import subprocess
import sys
import time

import serial

ESPTOOL = os.path.expanduser("~/.platformio/packages/tool-esptoolpy/esptool.py")


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    ports = glob.glob("/dev/cu.usbmodem*")
    if not ports:
        sys.exit("The board isn't on USB. Hold its button and try again.")
    port = ports[0]

    subprocess.run(
        [sys.executable, ESPTOOL, "--port", port, "--before", "default_reset", "--after", "hard_reset", "chip_id"],
        capture_output=True,
        check=True,
    )
    link = serial.Serial()
    link.port, link.baudrate, link.timeout, link.dtr, link.rts = port, 115200, 0.2, False, False
    deadline = time.time() + 10
    while True:
        try:
            link.open()
            break
        except serial.SerialException:
            if time.time() > deadline:
                sys.exit("The board didn't come back after the reset.")
            time.sleep(0.1)

    deadline = time.time() + 6
    while time.time() < deadline and "boot:" not in link.readline().decode(errors="replace"):
        pass
    link.write(sys.argv[1].encode() + b"\n")

    name = sys.argv[1].split()[0] + ":"
    deadline = time.time() + 5
    while time.time() < deadline:
        line = link.readline().decode(errors="replace").strip()
        if line.startswith(name) or line.startswith("usage:"):
            print(line)
            return
    sys.exit("No answer from the board.")


if __name__ == "__main__":
    main()
