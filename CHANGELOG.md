# Changelog

What changed in each release. The server and the firmware share a version,
and every release has the server for each system and the firmware for each
board, see [Releases](https://github.com/Flopsstuff/voistic/releases).

## v0.3.1, 2026-11-06

- `groq.prompt` in the config is sent with every transcription: the names,
  terms and spellings to expect, up to Groq's 224 tokens. It guides the model,
  it doesn't instruct it.
- `groq.replace` swaps words and phrases in a transcript for how they should
  be spelled, whole ones only and in any case, longer phrases first, after
  `ignore`.
- `voistic help` and `voistic usb help` end with the version and the
  repository's address.

## v0.3.0, 2026-11-06

### A button on USB, from the server

`voistic usb` replaces `tools/board.py`: no Python, no esptool. Plug the button
in and press it once, and the server restarts it and keeps it awake, with its
button ignored, until the command is done.

- `voistic usb setup` puts a new button on this computer's Wi-Fi, with the
  password saved here (the keychain on macOS, `netsh` on Windows, `nmcli` on
  Linux), pairs it and checks that it reaches the server.
- `voistic usb pair` pairs over the cable, with no `voistic pair` window and no
  mDNS.
- `voistic usb` shows what the button says about itself: firmware, network,
  address, signal, server, battery and brightness.
- `voistic usb wifi`, `check`, `brightness`, `update`, `log` and `reset`, see
  `voistic usb help`.
- `voistic help` lists every command.

### Updates from GitHub releases

- `voistic update` updates the server from the latest release, signs it on
  macOS like `make build` does, so the firewall keeps its approval, and
  restarts the installed agent or task.
- `voistic firmware <device> latest` offers a button the latest release's
  firmware for its board, which it takes on 5 presses. A XIAO gets the one with
  a microphone unless `--no-mic` says otherwise.
- `voistic usb update` installs it at once over Wi-Fi, without the presses, and
  `voistic usb update <file>` a build of your own.
- Every download is checked against the release's `SHA256SUMS`. The server
  reads a release image's version from the image, such as
  `v0.3.0+1106140000`, since the tag alone never matches what the button
  reports, which would offer it again on every check.

### No press is lost

- A press made while the last one was still being answered, or while its
  green fade played, used to be thrown away, so a click right after a
  recording often took two or three tries, the XIAO's especially. Presses are
  now kept with their times and counted.
- After a press the button stays fully awake for 30 s, with Wi-Fi and the
  microphone on, so the next press needs no wake or Wi-Fi join. Set it with
  `stay_ready` for each device in the config, `0s` turns it off. After that the
  XIAO deep-sleeps and the IoT Button V2 waits in light sleep as before.
- Waking from light sleep, the IoT Button V2 starts its microphone before
  anything else, so the start of a sentence isn't cut.

### Fixes

- A firmware stayed on offer after the button installed it, so 5 presses
  could later take a button that had moved on back to it. The offer is now
  withdrawn once the button runs it, and when a button is paired over USB.
- Saving a network over USB while the button was joining another made the
  Wi-Fi driver refuse it for a moment.
- The full reset had no way back: once the hold reached 15 s, letting go at
  any time reset the button. Now the window closes at 20 s, and a hold past it
  cancels at once, with the light off.

### Upgrading

- A server older than this has no `voistic update`, so download this release
  by hand once. The ones after it update themselves.
- Buttons on older firmware keep working, and take this one over Wi-Fi:
  `voistic firmware <device> latest`, then 5 presses. `voistic usb` needs this
  firmware on the button.

## v0.2.1, 2026-11-04

- `unpaste: true`, an action that erases the last pasted transcript with as
  many backspaces, which also works in a terminal, where Cmd+Z doesn't undo a
  paste.

## v0.2.0, 2026-11-03

- The Seeed IoT Button V2 (ESP32-C6) as a second board, with an analog
  microphone on its test pads and an 18650. It stays connected between presses
  in light sleep, and deep-sleeps after `deep_sleep_after`.
- The server tells boards apart, keeps each button's board and battery, and
  refuses a firmware for another chip than the button's.

## v0.1.1, 2026-11-02

- mDNS stays on the primary network card, rather than a virtual adapter of
  WSL or a VM, and each `voistic pair` renews it.
- A button pairs with the server on its own subnet, and a 2 s press cancels
  pairing.

## v0.1.0, 2026-11-02

The first release: the XIAO ESP32S3 button with gestures and voice, the server
for macOS, Windows and Linux with `key`, `exec` and `http` actions, Groq
transcription pasted into the active window, `paste_suffix`, pairing on request
only, updates over Wi-Fi, and the printed case.
