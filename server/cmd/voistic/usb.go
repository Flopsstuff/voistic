package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"voistic/server/internal/control"
	"voistic/server/internal/release"
	"voistic/server/internal/usb"
	"voistic/server/internal/wlan"
)

const usbUsage = `usage: voistic usb <command> [-v]

Works on a button plugged into this computer. Plug it in and press its button
once, so it wakes and its port appears: voistic waits for it, restarts it and
keeps it awake, with its button ignored, until the command is done.

  status                   what the button says about itself (the default)
  setup                    all a new button needs: this computer's Wi-Fi, pairing, a check
  wifi [ssid [password]]   put it on a network, by default the one this computer is on,
                           with the password saved here (the keychain on macOS)
  pair                     pair it with this server, no pairing window needed
  check                    make it ask the server, which proves Wi-Fi and key
  brightness <0..1>        set its light, 0.2 by default
  update [file [version]]  install the latest release's firmware, or a file, over Wi-Fi.
                           A file from pio has its version.txt next to it
  log                      follow what it prints, across sleeps, until Ctrl-C
  reset                    make it forget its Wi-Fi and its server
  help                     this text

  -v                       also show what the button prints along the way

Examples:
  voistic usb setup
  voistic usb wifi "Home" secret
  voistic usb update .pio/build/c6/firmware.bin
`

// waitTimeout is how long voistic usb waits for a button to appear.
const waitTimeout = 2 * time.Minute

func runUSB(p paths, client *control.Client, args []string) error {
	verbose := false
	var rest []string
	for _, arg := range args {
		if arg == "-v" {
			verbose = true
		} else {
			rest = append(rest, arg)
		}
	}
	command := "status"
	if len(rest) > 0 {
		command, rest = rest[0], rest[1:]
	}
	if command == "help" || command == "-h" || command == "--help" {
		printHelp(os.Stdout, usbUsage)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if command == "log" && len(rest) == 0 {
		return followLog(ctx)
	}

	var run func(context.Context, *usb.Board, usb.Status) error
	switch {
	case command == "status" && len(rest) == 0:
		run = func(context.Context, *usb.Board, usb.Status) error { return nil }
	case command == "setup" && len(rest) == 0:
		run = func(ctx context.Context, b *usb.Board, status usb.Status) error {
			status, err := setWiFi(ctx, b, nil)
			if err != nil {
				return err
			}
			if !status.Connected {
				return errors.New("it can't join that network, so it can't pair yet")
			}
			if err := pair(ctx, client, b, status); err != nil {
				return err
			}
			return checkServer(ctx, b)
		}
	case command == "wifi" && len(rest) <= 2:
		run = func(ctx context.Context, b *usb.Board, _ usb.Status) error {
			_, err := setWiFi(ctx, b, rest)
			return err
		}
	case command == "pair" && len(rest) == 0:
		run = func(ctx context.Context, b *usb.Board, status usb.Status) error {
			if err := pair(ctx, client, b, status); err != nil {
				return err
			}
			return checkServer(ctx, b)
		}
	case command == "check" && len(rest) == 0:
		run = func(ctx context.Context, b *usb.Board, _ usb.Status) error { return checkServer(ctx, b) }
	case command == "brightness" && len(rest) == 1:
		value, err := strconv.ParseFloat(rest[0], 64)
		if err != nil || value < 0 || value > 1 {
			return errors.New("brightness goes from 0 to 1, such as 0.2")
		}
		run = func(ctx context.Context, b *usb.Board, _ usb.Status) error {
			if err := b.SetBrightness(ctx, value); err != nil {
				return err
			}
			fmt.Printf("Brightness %.2f, saved on the button.\n", value)
			return nil
		}
	case command == "update" && len(rest) <= 2:
		run = func(ctx context.Context, b *usb.Board, status usb.Status) error {
			version, err := updateOverUSB(ctx, p, b, status, rest)
			if err == nil && version != "" {
				b.Close() // it restarted, there's no session left to end
				return watchConfirmation(ctx, client, status.Device, version)
			}
			return err
		}
	case command == "reset" && len(rest) == 0:
		run = func(ctx context.Context, b *usb.Board, _ usb.Status) error {
			if err := b.Reset(ctx); err != nil {
				return err
			}
			fmt.Println("It forgot its Wi-Fi and its server. Set it up again with voistic usb setup.")
			return nil
		}
	default:
		printHelp(os.Stderr, usbUsage)
		os.Exit(2)
	}

	board, status, err := connect(ctx, verbose)
	if err != nil {
		return err
	}
	printStatus(status)
	err = run(ctx, board, status)
	if command == "update" && err == nil {
		return nil // ended by the restart, or by the board saying it runs it already
	}
	if endErr := board.End(context.Background()); err == nil && endErr != nil {
		err = fmt.Errorf("the button may stay awake: %w", endErr)
	}
	return err
}

func connect(ctx context.Context, verbose bool) (*usb.Board, usb.Status, error) {
	if ports, _ := usb.Ports(); len(ports) == 0 {
		fmt.Println("Waiting for a button on USB. Press it once, so it wakes...")
	}
	ctx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	var log func(string)
	if verbose {
		log = func(line string) { fmt.Fprintln(os.Stderr, "  | "+line) }
	}
	board, status, err := usb.Connect(ctx, log)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, status, errors.New("no button showed up on USB. Is the cable one for data?")
	}
	return board, status, err
}

func printStatus(s usb.Status) {
	name := s.Board
	if s.Mic {
		name += ", with a microphone"
	}
	fmt.Printf("%s (%s), firmware %s\n", s.Device, name, s.Firmware)
	wifi := "no network saved"
	if s.SSID != "" {
		wifi = s.SSID + ", not connected"
	}
	if s.Connected {
		wifi = fmt.Sprintf("%s, %s, signal %d dBm", s.SSID, s.IP, s.RSSI)
	}
	fmt.Printf("  Wi-Fi       %s\n", wifi)
	server := "not paired"
	if s.Paired {
		server = "paired, last reached at " + s.Server
		if s.Server == "" {
			server = "paired, not reached yet"
		}
	}
	fmt.Printf("  server      %s\n", server)
	if s.Battery > 0 {
		fmt.Printf("  battery     %.2f V\n", float64(s.Battery)/1000)
	}
	fmt.Printf("  brightness  %.2f\n", s.Brightness)
}

// setWiFi saves a network on the board and waits for it to join. With no name,
// it's the one this computer is on, and with no password, the one saved here.
func setWiFi(ctx context.Context, b *usb.Board, args []string) (usb.Status, error) {
	var ssid, password string
	if len(args) > 0 {
		ssid = args[0]
	} else {
		network, err := wlan.Current()
		if err != nil {
			return usb.Status{}, fmt.Errorf("%w, name it: voistic usb wifi <ssid>", err)
		}
		ssid = network.SSID
		if network.Guessed {
			fmt.Printf("The system hides this computer's network, taking the last one joined: %s.\n", ssid)
			fmt.Println("Name another with voistic usb wifi <ssid>.")
		}
	}
	if len(args) > 1 {
		password = args[1]
	} else {
		var err error
		if password, err = wlan.Password(ssid); err != nil {
			if password, err = askPassword(ssid); err != nil {
				return usb.Status{}, err
			}
		}
	}
	if err := b.SetWiFi(ctx, ssid, password); err != nil {
		return usb.Status{}, err
	}
	fmt.Printf("Saved %s on the button, joining...\n", ssid)
	status, err := b.WaitConnected(ctx, 20*time.Second)
	if err != nil {
		return status, err
	}
	if !status.Connected {
		fmt.Println("It hasn't joined. Check the password, and that the network has 2.4 GHz: the ESP32 has no 5 GHz.")
		return status, nil
	}
	fmt.Printf("Joined %s as %s, signal %d dBm.\n", ssid, status.IP, status.RSSI)
	return status, nil
}

func askPassword(ssid string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no saved password for %s here, give it: voistic usb wifi %q <password>", ssid, ssid)
	}
	fmt.Printf("Password for %s: ", ssid)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(password), err
}

func pair(ctx context.Context, client *control.Client, b *usb.Board, status usb.Status) error {
	adopted, err := client.Adopt(control.AdoptRequest{Device: status.Device, Board: status.Board, Firmware: status.Firmware})
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(adopted.Address)
	if err != nil {
		return err
	}
	if err := b.Adopt(ctx, adopted.Key, host, port); err != nil {
		return err
	}
	fmt.Printf("Paired %s with this server at %s.\n", status.Device, adopted.Address)
	return nil
}

func checkServer(ctx context.Context, b *usb.Board) error {
	answer, err := b.Check(ctx)
	if err != nil {
		return err
	}
	switch answer {
	case "ok":
		fmt.Println("The server answered: Wi-Fi and pairing work.")
	case "update waiting":
		fmt.Println("The server answered, and holds a new firmware for it: voistic usb update.")
	default:
		return errors.New(answer)
	}
	return nil
}

// updateOverUSB queues a firmware for the button on this server, then has it
// fetch it over Wi-Fi at once, rather than on 5 presses. It returns the version
// it installed, after which the board restarts, or "" if there was nothing to
// install.
func updateOverUSB(ctx context.Context, p paths, b *usb.Board, status usb.Status, args []string) (string, error) {
	if !status.Paired {
		return "", errors.New("it isn't paired with this server, which is where it takes the firmware from: voistic usb pair")
	}
	var file, version string
	var image []byte
	switch len(args) {
	case 0:
		var err error
		if image, version, err = latestFirmware(ctx, status.Board, status.Mic); err != nil {
			return "", err
		}
		if version == status.Firmware {
			fmt.Printf("It already runs %s, the latest release.\n", version)
			b.End(ctx)
			return "", nil
		}
	case 1:
		file = args[0]
		data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "version.txt"))
		if err != nil {
			return "", errors.New("no version.txt next to the file, give the version: voistic usb update <file> <version>")
		}
		version = strings.TrimSpace(string(data))
	default:
		file, version = args[0], args[1]
	}
	if file == "" {
		var err error
		if file, err = writeTemp(image); err != nil {
			return "", err
		}
		defer os.Remove(file)
	}
	info, err := firmwares(p).Add(status.Device, status.Board, file, version)
	if err != nil {
		return "", err
	}
	fmt.Printf("Installing %s, %d KB, over Wi-Fi...\n", info.Version, info.Size/1024)
	answer, err := b.Update(ctx)
	if err != nil {
		return "", err
	}
	if answer == "current" {
		fmt.Println("It already runs that firmware.")
		b.End(ctx)
		return "", nil
	}
	fmt.Println("Installed, it restarts and checks in with the server...")
	return info.Version, nil
}

// watchConfirmation waits for the restarted button to report the new firmware
// to the server, which is when it keeps it. Its log can't tell: the line goes
// out at boot, before the port is open again.
func watchConfirmation(ctx context.Context, client *control.Client, device, version string) error {
	ctx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	for {
		devices, err := client.Devices()
		if err == nil {
			for _, d := range devices {
				if d.ID == device && d.Firmware == version {
					fmt.Printf("It runs %s and reached the server, so it keeps it.\n", version)
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("the button didn't report the new firmware within a minute. If it couldn't reach the server, it went back to the old one: voistic devices shows what it runs")
		case <-time.After(time.Second):
		}
	}
}

// confirmTimeout covers the restart, the join and the first check.
const confirmTimeout = time.Minute

func latestFirmware(ctx context.Context, board string, mic bool) ([]byte, string, error) {
	name, err := release.FirmwareName(board, mic)
	if err != nil {
		return nil, "", err
	}
	latest, err := release.Latest(ctx)
	if err != nil {
		return nil, "", err
	}
	image, err := latest.Fetch(ctx, name)
	if err != nil {
		return nil, "", err
	}
	version, err := release.FirmwareVersion(image, latest.Tag)
	if err != nil {
		return nil, "", err
	}
	return image, version, nil
}

func followLog(ctx context.Context) error {
	if ports, _ := usb.Ports(); len(ports) == 0 {
		fmt.Println("Waiting for a button on USB. Press it, so it wakes...")
	}
	return usb.Follow(ctx, func(line string) {
		fmt.Printf("%s %s\n", time.Now().Format("15:04:05.000"), line)
	}, func() {
		fmt.Println("--- off USB, asleep or unplugged, waiting for it to come back")
	})
}
