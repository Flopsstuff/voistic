# AGENTS.md

Guidance for AI coding agents working in this repository.
`CLAUDE.md` and `GEMINI.md` are symlinks to this file.

A physical Wi-Fi remote. A battery-powered ESP32 button sleeps, wakes on a press, recognises a gesture, sends it to a server on the LAN and goes back to sleep. The server runs whatever the config binds to that gesture. A hold records the voice, which the server transcribes and pastes into the active window.

See `README.md` for usage, `docs/hardware.md` for how the XIAO button is built and `docs/iot-button-c6.md` for the IoT Button V2.

## Commands

Firmware (PlatformIO, from the repo root):

```bash
pio run                                     # build
pio run -t upload --upload-port /dev/cu.usbmodem14301
pio run -e mic -t upload --upload-port /dev/cu.usbmodem14301   # the XIAO with a microphone
pio run -e c6                               # the IoT Button V2, the first build takes minutes, see below
pio test -e native                          # gesture tests, run on this computer
pio test -e native -f test_gesture          # a single test suite
tools/publish-firmware.sh mic voistic-9af4  # build and offer it over the air, the button asks on 5 presses
tools/publish-firmware.sh c6 voistic-25bc
python tools/board.py "brightness 0.2"      # a command for the board over USB, with pyserial
```

Server (Go, from `server/`):

```bash
make build            # bin/voistic, signed with the "Voistic Local" certificate on macOS
make restart          # build and restart the launchd agent
make test             # go vet + go test
make dist             # an archive each for darwin, linux and windows, amd64 and arm64
go test ./internal/api -run TestPairGivesWorkingKey   # a single test
```

```bash
voistic serve | pair | forget <device> | devices | test <device> <gesture> | check | transcribe <file> | firmware <device> <file> <version> | install | uninstall | version
```

Config lives in `~/.config/voistic/config.yaml` (`VOISTIC_CONFIG`), state with device keys in `~/.local/state/voistic/` (`VOISTIC_STATE`).

## Hardware

Two boards, each an env in `platformio.ini`, all built with pioarduino's platform and Arduino core 3. What differs between them is in `src/board.*`, the `#ifdef VOISTIC_BOARD_C6` in `leds.cpp`, and the microphone file.

Seeed XIAO ESP32S3 Plus (16 MB flash, 8 MB PSRAM) with an Adafruit IoT Button with NeoPixel BFF mounted back to back, and a LiPo. Envs `usb` and `mic`, board `xiao-s3`.

- Button on A2 (GPIO3), shorts to GND. NeoPixel on A3 (GPIO4), powered directly from 3.3V, so it keeps its last colour until told otherwise, even through a reset or deep sleep.
- User LED on GPIO21, active low. The red charge LED is driven by the charger, not firmware, and blinks while charging.
- The `mic` build adds an SPH0645 on I²S (`mic.cpp`): BCLK D7 (GPIO44), LRCL D9 (GPIO8), DOUT D8 (GPIO7), SEL on GND, so the left channel. It needs at least 32 kHz.
- No battery measurement: `ADC_BAT` (GPIO10) is only a bottom pad with no divider, and the charger status isn't wired to a GPIO.

Seeed IoT Button V2 (ESP32-C6FH4, 4 MB flash, no PSRAM) with an analog DFRobot SEN0487 on its test pads (`mic_adc.cpp`) and an 18650. Env `c6`, board `iot-button-c6`.

- Button on GPIO2. RGB LED data on GPIO19, its power switch on GPIO18. Red/blue LED on GPIO14/GPIO3, active low.
- Battery through a 20k/10k divider on GPIO1, connected only while GPIO0 is high.
- Microphone: VCC from GPIO7, output on GPIO6 (ADC1 channel 6), read in continuous mode.
- It stays connected between presses in light sleep, and deep-sleeps after `deep_sleep_after` from the server's config.

## Architecture

The firmware runs one short cycle per press rather than a long-lived loop: `src/main.cpp` starts Wi-Fi at once, waits for the gesture, sends it, shows the outcome and ends with `finish()`. There is no way to reach a sleeping board, which shapes everything else.

On the XIAO, `finish()` enters deep sleep. On the C6 (`kBoardStaysConnected`), each press runs in its own FreeRTOS task, which `finish()` deletes, and `loop()` waits for the next press in automatic light sleep with Wi-Fi in power save, woken by a level interrupt on the button. After the time in the last answer's `awake_s` it deep-sleeps. That light sleep needs `CONFIG_PM_ENABLE`, which Arduino's prebuilt ESP-IDF lacks, so `env:c6` rebuilds ESP-IDF with `sdkconfig.c6.defaults` through pioarduino's `custom_sdkconfig`.

- `gesture.*` is the gesture state machine, fed by timestamped edges. It has no Arduino dependency, so it's what `pio test -e native` covers. The gesture table is at the top of `gesture.h`.
- `leds.*` draws from its own FreeRTOS task, so animations keep running during blocking HTTP calls. Every state's look is one row of `kPatterns`, in `Led` enum order. A brightness set once over USB scales them all and stays in NVS.
- `board.*` holds what differs between boards: pins, waking, the battery and what's switched off for sleep.
- `link.*` caches channel and BSSID in RTC memory, which skips the scan: 0.3-0.8 s to connect instead of 3.7 s. Modem sleep is off, which halves the median join. An access point that refuses the first association, after a crash or a reflash, gets a retry a second later.
- `client.*` holds the key, seq and server address in NVS, signs requests and finds the server over mDNS only after a cached address fails, since a query takes seconds. It also warms the ARP cache right after the join, because resolving the server on the first connection cost 100-400 ms.
- `portal.*` is the access-point Wi-Fi setup page.
- `mic.*`, only with `VOISTIC_MIC`, captures from the wake into a ring buffer, so the voice since the press isn't lost while the network comes up: 6 MB in PSRAM on the XIAO (`mic.cpp`), 128 KB on the C6 (`mic_adc.cpp`).

The server (`server/internal/`) splits into `api` (the LAN protocol), `config` (hand-edited YAML, re-read on every press), `store` (machine-written keys, seq, board and battery, mode 600), `board` (board names and their chips), `service` (the launchd agent on macOS, a scheduled task on Windows), `firmware` (the image each button should run), `action` (`key`, `exec` and `http`), `recording` (voice into WAV files), `stt` (Groq speech-to-text), `paste` (types a transcript and presses keys, through `osascript` on macOS and Win32 on Windows, with a file for each system, and remembers the last paste's length, so `unpaste` can erase it with as many backspaces), `pairing` (the `voistic pair` window), `discovery` (mDNS `_voistic._tcp`) and `control` (the CLI over a Unix socket).

Protocol: JSON over HTTP on port 8787, signed with HMAC-SHA256 in `X-Voistic-Sig`, both ways. `seq` must grow, and a retry of the latest seq gets the stored answer without running the action again. The server answers before the action finishes, because the button is waiting to sleep. Requests carry `board`, and `battery_mv` where it's measured, which the store keeps. Answers to a press or a recording carry `awake_s`, from `deep_sleep_after` in the config, and `seq` stays the last field, since the firmware looks for it at the end.

Voice: `POST /v1/audio?device=&seq=&rate=` is a chunked stream of 16-bit mono PCM for as long as the button is held. The signature covers `audio|device|seq|rate`, since the body isn't known yet, so the audio itself isn't authenticated. The server's read deadline moves with each read, because a recording outlasts the 5 s request limit, and the file goes to `recordings` from the config. Afterwards the server transcribes it in the background, using `groq` from the config, into a `.txt` next to the WAV, and with `paste: true` puts it on the clipboard and presses Cmd+V. With `save_recordings: false` the WAV and the text are deleted afterwards, even if transcription failed.

Updates: five short presses send a signed `POST /v1/update`. If the server holds a different version for that button, the signed answer carries its version, size and SHA-256, and the button fetches it from `GET /v1/firmware` into the free OTA slot, checks the hash and restarts. A new firmware stays on probation (`verifyRollbackLater` returns true) until `confirmFirmware` reaches the server, and otherwise the bootloader goes back. `scripts/version.py` stamps every build with a version, so a rebuild always counts as different. `voistic firmware` refuses an image whose chip, from its header, isn't the button's.

Pairing: `POST /v1/pair` only succeeds for the device claiming an open `voistic pair` window, so it's always asked for on the server, and with several servers on a network the rest refuse. The firmware asks every server that answers mDNS until one takes it, on the address in its own subnet, and a 2 s press gives up. A button gives up its key through a signed `POST /v1/unpair`, which the firmware does on its own.

CI: `.github/workflows/ci.yml` runs `go vet` and `go test` on Linux, macOS and Windows and builds the firmware. `release.yml`, on a `v*` tag, runs those, then `make dist` and the firmware, and publishes a GitHub release with `SHA256SUMS`. Nothing in CI can press keys, since a runner has no desktop, so pasting on Windows only gets tried on a real machine.

## Things that bite

- **`Serial.setTxTimeoutMs(0)` hangs the board.** `HWCDC::write` counts down from it, so 0 wraps around once the TX buffer fills with no reader. Keep it non-zero.
- **Opening the serial port resets the board**, and deep sleep removes the port altogether. A serial log can't span a wake, so send diagnostics over the network instead.
- **To flash, the board must be awake.** Holding the button keeps it awake: a hold only ends on release.
- **`WiFi.SSID()` is empty until connected.** Read the saved network with `esp_wifi_get_config`.
- **The macOS firewall silently drops LAN connections** to binaries it hasn't approved: the connection opens and then breaks, or connections stall for up to half a minute after a rebuild or a move, until it adds its rule. It affects any inbound listener, so `make build` signs with a fixed certificate (`server/scripts/make-signing-cert.sh`), and PlatformIO is installed on the pyenv Python, which the firewall allows.
- **`WiFi.mode()` blocks the loop for tens of milliseconds**, which is why button edges are captured in an interrupt.
- **mDNS only works on the primary network card.** With no interface named, the system picks where to join the group, which can be the virtual adapter of WSL or a VM, and the server then hears nothing and logs no error. So `discovery` takes the card of the default route, and only its address goes out. The socket can go deaf too: on macOS the first run after a rebuild doesn't hear mDNS. So each `voistic pair` renews it, and pairing needs no restart.
- **A Windows task has to be interactive.** A service can't reach the desktop, so `install` makes a scheduled task in the user's session. It runs `voisticw.exe`, built with `-H=windowsgui`, because a console program would keep a window open, and with no console `serve --log` is what keeps the log.
- **Arduino core 3 changed some units.** `WiFiClient::setTimeout` takes milliseconds now, not seconds, and a timeout of 3 meant 3 ms.
- **The ADC driver's lock belongs to a task.** On the C6, starting continuous mode in one task and stopping it in another trips a FreeRTOS assert, so `mic_adc.cpp` does both in its capture task.
- **ESP-IDF 5 choices take the chosen option.** `# CONFIG_X is not set` doesn't leave a choice, such as the DHCP check, so `sdkconfig.c6.defaults` sets the wanted one to `y`. A `custom_sdkconfig` written inline in `platformio.ini` lost such lines, so it's a file.
- **Power management arms a timer for light sleep, which survives into deep sleep.** The C6 then woke at once, every time. `boardSleep()` clears every wake source before arming the button.
- **pioarduino's custom build runs PlatformIO from `~/.platformio/penv`.** Version `6.2.0.post1` there counted as older than the platform's minimum, and the build removed the platform. Pinning that copy to `6.2.0` fixed it.
- `clang` diagnostics for `src/` are false alarms, since no `compile_commands.json` exists. Trust `pio run`.
