# IoT Button V2

The second voistic button, board `voistic-25bc`, is a Seeed IoT Button V2 in its own case, with an analog microphone soldered to its test pads and an 18650 cell. Build the firmware with `pio run -e c6`.

Unlike the XIAO button, it doesn't go into deep sleep after every press. It waits for the next one in light sleep with Wi-Fi connected, so a press reaches the server in tens of milliseconds, and it deep-sleeps after `deep_sleep_after` from the config, 30 minutes by default.

The research that led here, with the diagnostic firmware and the tools, is in [`research/iot-button-c6`](../research/iot-button-c6).

## Parts

| Part | Role |
|---|---|
| Seeed IoT Button V2, board v2.0 of 24/04/2025 | ESP32-C6FH4 with 4 MB flash and no PSRAM, button, RGB LED, red/blue LED, charger, battery switch |
| DFRobot SEN0487 (Fermion MEMS microphone) | voice, analog, with its own amplifier |
| 18650 cell | power |

The [schematic](https://files.seeedstudio.com/wiki/IoT_Botton_ESPHOME/Seeed%20IoT%20Button-C6%20v2.0_SCH_250721.pdf) and the [KiCad sources](https://files.seeedstudio.com/wiki/IoT_Botton_ESPHOME/Seeed%20IoT%20Button-C6%20v2.0_SCH%26PCB_250721.zip) are on Seeed's wiki.

## Pins

| Function | GPIO | |
|---|---|---|
| Main button, to GND with a 10k pull-up | 2 | wakes the board |
| RGB LED data | 19 | |
| RGB LED power, a load switch | 18 | high is on |
| Red LED | 14 | low is on |
| Blue LED | 3 | low is on |
| Battery divider switch | 0 | high connects a 20k/10k divider |
| Battery voltage, a third of it | 1 | ADC1 channel 1 |
| BOOT button | 9 | |

## Test pads

Ten round pads on the back, seen from the back with USB-C on the right. Every one was checked: the power pads with a multimeter, the GPIOs by firmware holding each high for its own number of seconds.

```
      1        2       3      4       5
   GPIO16    3.3V    GPIO6   GND    GPIO7
  (UART TX)

      6        7       8      9      10
   GPIO17    BOOT    GPIO4   EN     GPIO5
  (UART RX)
```

Pad 1 only rose to about 1.5 V when driven high, which the schematic doesn't explain. GPIO16 and 17 carry the UART0 log at boot, and GPIO4 and 5 are strapping pins.

## Microphone

| Microphone | Pad | |
|---|---|---|
| VCC | 5, GPIO7 | powered from the pin, so it's off in sleep |
| GND | 4 | |
| A | 3, GPIO6 | ADC1 channel 6, which isn't a strapping pin |

- The output idles at 1.50 V, mid-range for the ADC at 12 dB attenuation.
- ADC1 runs in continuous mode at 64 kHz, and every 4 samples are summed into one at 16 kHz, 14 bits.
- A high-pass filter removes the offset, and x4 brings the values to 16 bits. Speech at hand's length then peaks between -8 and -17 dBFS.
- The first 0.1 s after power-up is dropped and the next 0.2 s fades in.
- With no PSRAM, the buffer is 128 KB, 4 s, which covers the time to connect.
- Wi-Fi sending during a recording raised the quietest parts slightly and left no hum at the packet rate. Groq transcribed test phrases with and without it.
- Touching the case while recording is loud, so the module is best held on foam.

## Lights

The RGB LED shows the same colours as the XIAO's pixel. The red/blue LED beside it shows what goes on in the background:

| | RGB | Red/blue |
|---|---|---|
| Hold, recording | violet at once, since the microphone records from the wake | blue blinks fast until the server is reached |
| Pairing | off | blue and red in turn, a siren |
| Battery below 3.5 V | | two red blinks before sleep |

## Power

- The board measures its battery at each wake by closing the divider switch for a moment, and sends it with every press. `voistic devices` shows it.
- Between presses it waits in automatic light sleep with Wi-Fi in power save. That needs ESP-IDF built with power management, which Arduino's own build lacks, so `env:c6` rebuilds it with [`sdkconfig.c6.defaults`](../sdkconfig.c6.defaults). The same file turns off the second-long check of a DHCP address.
- No light sleep while USB is connected, so the port stays for logs and flashing. The Wi-Fi connection is kept all the same.
- In deep sleep the RGB LED's load switch and the microphone are off. **Nobody has measured the current** in either sleep yet.

## Quirks

- The factory firmware is Seeed's Zigbee firmware. A full backup of the 4 MB flash was taken before replacing it.
- After a crash or a reflash, the access point may still count the board as connected and refuse it once. The firmware tries again after a second.
- Its log goes to USB, and the port follows the board in and out of deep sleep.
