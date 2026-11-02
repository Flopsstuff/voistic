# voistic

![A lighter-sized voice stick next to a laptop, sending speech to a prompt](docs/images/hero.jpg)

*Concept render. The real one is 3D printed from the models in [`cad/`](cad), and shown below.*

A physical Wi-Fi button that talks to your computer. Hold it and speak: the audio streams to a server on your LAN, gets transcribed and is typed into the active window. Short presses run whatever you bind to them, such as Enter or Tab.

The button is an ESP32-S3 on a small LiPo. It sleeps between presses, wakes on the button, does its job and goes back to sleep. The server is a single Go binary for macOS, Windows and Linux.

[Watch a short video](docs/images/vid.mp4) of the button typing a message by voice.

![The parts before stacking: the XIAO board with its antenna, the microphone, the button board and the battery](docs/images/0.jpg)

| | |
|---|---|
| ![The printed case in a hand, with the button and the microphone slit on its front](docs/images/7.jpg) | ![The case in a hand in front of this repository, with the pixel glowing through the slit](docs/images/8.jpg) |

## How it works

1. A press wakes the board. Wi-Fi and the microphone start at once, before the gesture is known.
2. The firmware recognises the gesture. A hold streams the voice to the server, starting from the moment of the wake, so nothing said before the link came up is lost.
3. The server saves a WAV, sends it to Groq for speech-to-text, writes the text next to it and pastes it into the active window.
4. For a short press, the server runs the action bound to it in the config.

## Gestures

| Gesture | How | Default on the microphone button |
|---|---|---|
| `single`, `double`, `triple` | 1, 2 or 3 presses, up to 350 ms apart | Enter, Tab, Esc |
| `long` | hold from 0.4 s, for as long as you speak | record and transcribe |
| Update | 5 short presses | fetch a new firmware from the server, if there is one |
| Pairing | 5 short presses, then hold the sixth for 5 s | a 2 s press cancels it |
| Full reset | 5 short presses, then hold the sixth for 15 s | forgets Wi-Fi and the server |

The pixel shows what the board understood:

| Pixel | Meaning |
|---|---|
| Fast blink, light blue, sea green, lime, yellow | 1st, 2nd, 3rd, 4th and later press |
| Fast blue blink | long press recognised, no link yet |
| Steady violet | recording |
| Green fading out | the server accepted it |
| Red fading out | something failed, the reason is in the server log |
| Blue, slowly breathing | taking a new firmware |

The blink is at full brightness while the button is down and at half when it's up. Letting go during the blue blink cancels the press.

## Setup

You need a [Groq API key](https://console.groq.com). [Releases](https://github.com/Flopsstuff/voistic/releases/latest) have the server for each system and the firmware. To build them yourself you need Go and [PlatformIO](https://platformio.org). The hardware is described in [docs/hardware.md](docs/hardware.md).

### Server

Download the archive for your system, or build it. On macOS, create the signing certificate first. macOS ties firewall and Accessibility approval to the signature, so a stable one keeps them across rebuilds:

```bash
cd server
scripts/make-signing-cert.sh
make build
```

Write the config at `~/.config/voistic/config.yaml`:

```yaml
listen: ":8787"
recordings: /path/to/recordings   # folder for WAV and text files
save_recordings: true             # false deletes them once they've been used
paste: true                       # type the text into the active window, macOS and Windows
paste_suffix: " "                 # added after the pasted text, so the next phrase doesn't stick to it
groq:
  api_key: your-groq-key
  language: en                    # leave out to let the API guess
  ignore:                         # phrases to cut, Whisper makes some up on silence
    - Thanks for watching
  # model: whisper-large-v3-turbo # defaults to whisper-large-v3
devices:
  voistic-9af4:                   # the button's id, voistic- and the end of its MAC
    name: Button with a microphone
    gestures:
      single:
        key: enter
      double:
        key: tab
      triple:
        key: esc
```

The file holds your API key, so keep it private with `chmod 600`. Edits apply on the next press. An action is one of `key`, which presses `enter`, `tab` or `esc` on macOS and Windows, `exec`, and `http`, with `url`, `method`, `headers` and `json` or `body`.

Then run the server at login, as a launchd agent on macOS:

```bash
bin/voistic install
```

Pasting and key presses need Accessibility permission. In System Settings, under Privacy & Security, Accessibility, add `server/bin/voistic`.

### Windows

Unpack the archive somewhere it can stay and put the config at `%USERPROFILE%\.config\voistic\config.yaml`. Then run the server at login, as a scheduled task:

```powershell
.\voistic.exe install
```

The task runs `voisticw.exe`, the same program with no console window, so keep the two files together. Windows SmartScreen warns about the unsigned files, and the firewall asks to allow the server on first start. The log is at `%USERPROFILE%\.local\state\voistic\serve.log`.

A window running as administrator can't be pasted into, since Windows blocks input to it from an ordinary program.

### Button

```bash
pio run -e mic -t upload --upload-port /dev/cu.usbmodem14301
```

The port only exists while the board is awake, so hold the button while it flashes. Use the default `usb` environment for a button with no microphone.

Tell the button your Wi-Fi network over USB within 10 s of a reset, as a serial line:

```
wifi network-name|password
```

The pixel brightness is set the same way, from 0 to 1, and stays on the board:

```
brightness 0.2
```

`tools/board.py` sends such a line for you, with the button held so the port exists:

```bash
python tools/board.py "brightness 0.2"
```

To pair a button, run `voistic pair` on the computer it should belong to, and within two minutes start pairing on the button. A new button does that on its first press, any other with 5 short presses and a 5 s hold on the sixth. The button only ever talks to that computer, so several computers and several buttons can share a network.

## Server commands

```
voistic serve [--log <file>]     run the server
voistic install | uninstall      run the server at login, or stop that, on macOS and Windows
voistic check                    validate the config
voistic devices                  list paired buttons
voistic pair                     accept one button
voistic forget <device>          drop a button's key
voistic test <device> <gesture>  run an action without the button
voistic transcribe <file>        turn a recording into text next to it
voistic firmware <device> <file> <version>  offer a firmware to a button
voistic version                  print the version
```

`make restart` rebuilds the server and restarts the agent. The log is at `~/.local/state/voistic/serve.log`.

## Updates over the air

After the first flash over USB, a new firmware goes over Wi-Fi:

```bash
tools/publish-firmware.sh mic voistic-9af4
```

This builds it and puts it on the server. Then press the button 5 times: it downloads the firmware, checks its hash and restarts. A new firmware has to reach the server on its first boot, or the bootloader goes back to the previous one.

A firmware from a release goes the same way, once you've run `voistic firmware voistic-9af4 firmware-mic.bin v0.1.0`, with the release's tag as the version.

## Security

Requests are signed with HMAC-SHA256 under a per-device key, and a growing counter stops replays. There's no TLS, so the voice crosses your LAN unencrypted, and only the start of a recording is signed, not the audio. Audio goes to Groq for transcription.

While `voistic pair` is waiting, anyone on the LAN can take that one place, so run it just before pairing. The Wi-Fi setup page is an open access point.

## Development

```bash
pio test -e native    # gesture tests, on this computer
cd server && make test
```

See [AGENTS.md](AGENTS.md) for the architecture. A push runs the tests on all three systems and builds the firmware. A tag like `v0.1.0` also publishes a release with every archive and firmware.

## License

[MIT](LICENSE)
