# Seeed IoT Button V2 (ESP32-C6): findings

Board marked `v2.0 4/24/2025`, schematic `Seeed IoT Button-C6 v2.0_SCH_250721.pdf`.

## Chip

ESP32-C6FH4 (QFN32), revision v0.2, 4 MB embedded flash, no PSRAM, 40 MHz crystal.
USB-Serial/JTAG. MAC `10:bd:a3:b1:25:bc`. eFuses clean: no secure boot, no flash
encryption, download mode and USB-JTAG enabled.

## Factory firmware

Seeed Zigbee firmware on Arduino core 3.2.1 (ESP-IDF v5.4.2), built 2025-06-29.
Tasks: ButtonTask, LedTask (FastLED), BatteryTask, SleepTask, Zigbee_main.
Logs to UART0, not USB. Full 4 MB backup taken (SHA-256 `bbae83bd...449590b6`),
restore with `esptool write-flash 0 flash_full.bin`.

## Pins

| Function | GPIO | Source |
|---|---|---|
| Main button K1, to GND, 10k pull-up | 2 | schematic |
| D8 red, on when LOW | 14 | **confirmed** |
| D8 blue, on when LOW | 3 | **confirmed** |
| RGB LED data | 19 | **confirmed** |
| RGB LED power switch, on when HIGH | 18 | **confirmed** |
| Battery divider switch (20k/10k, VBAT/3) | 0 | schematic |
| Battery ADC | 1 | schematic |
| BOOT button | 9 | schematic |
| Test pads TP14-TP17 | 4, 5, 6, 7 | schematic |
| Test pads TP19 (RX), TP20 (TX via 499R) | 17, 16 | schematic |

The board runs from USB with the battery disconnected.

## Test pads, back side, USB-C on the right

All confirmed: power pads with a multimeter, GPIOs by holding each HIGH for its
own number of seconds.

```
      1        2       3      4       5
   GPIO16    3.3V    GPIO6   GND    GPIO7
  (U0TXD)

      6        7       8      9      10
   GPIO17    BOOT    GPIO4   EN     GPIO5
  (U0RXD)  (GPIO9)
```

## Analog microphone (DFRobot SEN0487)

Wired to pads 5 (VCC from GPIO7), 4 (GND) and 3 (A, GPIO6, ADC1 channel 6).
ADC continuous mode at 64 kHz, attenuation 12 dB, 4 samples summed into
16 kHz (measured 16 024-16 025 Hz).

- Idle output 1.50 V, mid-range. Idle noise rms 5-15 (12-bit units).
- Speech at hand distance: rms 66-100, peaks -10 to -17 dBFS, no clipping.
- Sounds clean by ear, Groq transcribed all three test phrases, two with one
  misheard phrase. Tested with Wi-Fi off.
- With Wi-Fi on, modem sleep off and UDP at about 34 KB/s during recording
  (as voistic streams), in the closed case: two 6 s recordings transcribed
  without errors. The quietest 0.25 s had rms 76-133 against 58-72 with
  Wi-Fi off, the loudest-to-quietest ratio stayed 27-28 dB, and the quiet
  parts show no peaks at the 33 Hz packet rate or its harmonics.
- Touching the case while recording gives full-scale spikes, so the module
  needs to sit firmly, ideally on foam.

Pad 1 (GPIO16) only rose to about 1.5 V when driven HIGH, the rest to 3.3 V.
The schematic has a 499R in series to TP20 and nothing else, so this is not
explained yet.
