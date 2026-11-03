// Command voistic runs the server for voistic Wi-Fi buttons and manages it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"voistic/server/internal/action"
	"voistic/server/internal/api"
	"voistic/server/internal/board"
	"voistic/server/internal/config"
	"voistic/server/internal/control"
	"voistic/server/internal/discovery"
	"voistic/server/internal/firmware"
	"voistic/server/internal/pairing"
	"voistic/server/internal/paste"
	"voistic/server/internal/recording"
	"voistic/server/internal/service"
	"voistic/server/internal/store"
	"voistic/server/internal/stt"
)

const usage = `usage: voistic <command>

  serve [--log <file>]     run the server, logging to stderr or a file
  pair                     accept one button, for two minutes
  forget <device>          drop a button's key, so it can pair again
  devices                  list paired buttons
  test <device> <gesture>  run an action without pressing the button
  check                    validate the config
  transcribe <file>        turn a recording into text next to it
  firmware <device> <file> <version>  offer a firmware to a button, it asks on 5 presses
  install                  run the server at login, on macOS and Windows
  uninstall                stop running it at login
  version                  print the version

A button only pairs while voistic pair is waiting.

Gestures: single, double, triple, long.

Environment:
  VOISTIC_CONFIG  config file (default ~/.config/voistic/config.yaml)
  VOISTIC_STATE   state directory (default ~/.local/state/voistic)
`

const transcribeTimeout = 2 * time.Minute

// version is set at build time.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "voistic:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	paths, err := defaultPaths()
	if err != nil {
		return err
	}

	client := control.NewClient(paths.socket)
	switch command := args[0]; {
	case command == "serve" && len(args) == 1:
		return serve(paths, os.Stderr)
	case command == "serve" && len(args) == 3 && args[1] == "--log":
		log, err := os.OpenFile(args[2], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer log.Close()
		return serve(paths, log)
	case command == "version" && len(args) == 1:
		fmt.Println(version)
		return nil
	case command == "check" && len(args) == 1:
		return check(paths)
	case command == "firmware" && len(args) == 4:
		return addFirmware(paths, args[1], args[2], args[3])
	case command == "install" && len(args) == 1:
		return install(paths)
	case command == "uninstall" && len(args) == 1:
		return service.Uninstall()
	case command == "transcribe" && len(args) == 2:
		return transcribe(paths, args[1])
	case command == "pair" && len(args) == 1:
		fmt.Printf("Waiting %v for a button in pairing mode...\n", control.PairWindow)
		device, err := client.Pair()
		if err != nil {
			return err
		}
		fmt.Printf("Paired %s. Add it under devices in %s to give it actions.\n", device, paths.config)
		return nil
	case command == "forget" && len(args) == 2:
		if err := client.Forget(args[1]); err != nil {
			return err
		}
		fmt.Printf("Forgot %s.\n", args[1])
		return nil
	case command == "devices" && len(args) == 1:
		return listDevices(client)
	case command == "test" && len(args) == 3:
		return test(client, args[1], args[2])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	return nil
}

type paths struct {
	config string
	state  string
	socket string
}

func defaultPaths() (paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths{}, err
	}
	p := paths{config: os.Getenv("VOISTIC_CONFIG"), state: os.Getenv("VOISTIC_STATE")}
	if p.config == "" {
		p.config = filepath.Join(home, ".config", "voistic", "config.yaml")
	}
	if p.state == "" {
		p.state = filepath.Join(home, ".local", "state", "voistic")
	}
	p.socket = filepath.Join(p.state, "voistic.sock")
	return p, nil
}

func serve(p paths, output io.Writer) error {
	log := slog.New(slog.NewTextHandler(output, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	loader := config.NewLoader(p.config, log)
	cfg, err := loader.Get()
	if err != nil {
		return err
	}
	states, err := store.Open(filepath.Join(p.state, "state.json"))
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	advertiser, err := discovery.Start(port, log)
	if err != nil {
		listener.Close()
		return fmt.Errorf("mDNS: %w", err)
	}
	defer advertiser.Stop()

	window := &pairing.Window{}
	buttons := &api.Server{
		Config:     loader.Get,
		Store:      states,
		Pairing:    window,
		Firmware:   firmwares(p),
		Dispatch:   dispatcher(ctx, log),
		Transcribe: transcriber(ctx, loader, log),
		Log:        log,
	}
	server := &http.Server{
		Handler:      buttons.Handler(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	cli := &control.Server{Config: loader.Get, Store: states, Pairing: window, Refresh: advertiser.Refresh, Log: log}
	controlDone := make(chan error, 1)
	go func() { controlDone <- cli.Serve(ctx, p.socket) }()

	log.Info("serving", "port", port, "config", p.config)
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return <-controlDone
}

// dispatcher runs each action in the background and logs how it went. Actions
// outlive the request, since the button is already going back to sleep.
func dispatcher(ctx context.Context, log *slog.Logger) func(string, string, config.Action) {
	return func(device, gesture string, configured config.Action) {
		go func() {
			start := time.Now()
			output, err := action.Run(ctx, configured, action.Event{Device: device, Gesture: gesture})
			fields := []any{"device", device, "gesture", gesture, "took", time.Since(start).Round(time.Millisecond), "output", output}
			if err != nil {
				log.Error("action failed", append(fields, "err", err)...)
				return
			}
			log.Info("action done", fields...)
		}()
	}
}

// transcriber turns each recording into text in the background. A failure only
// costs the text, the recording is already safe.
func transcriber(ctx context.Context, loader *config.Loader, log *slog.Logger) func(string) {
	return func(path string) {
		go func() {
			cfg, err := loader.Get()
			if err != nil {
				log.Error("no usable config, not transcribing", "path", path, "err", err)
				return
			}
			if !cfg.KeepsRecordings() {
				defer func() {
					if err := recording.Remove(path); err != nil {
						log.Error("can't remove a recording", "path", path, "err", err)
					}
				}()
			}
			if cfg.Groq.APIKey == "" {
				log.Warn("no groq key in the config, not transcribing", "path", path)
				return
			}
			ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
			defer cancel()
			start := time.Now()
			out, text, err := speechClient(cfg).TranscribeFile(ctx, path)
			if err != nil {
				log.Error("transcription failed", "path", path, "err", err)
				return
			}
			if text == "" {
				log.Info("no speech in the recording", "path", path)
				return
			}
			log.Info("transcribed", "path", out, "took", time.Since(start).Round(time.Millisecond), "chars", len([]rune(text)))
			if cfg.Paste {
				if err := paste.Text(ctx, text+cfg.PasteSuffix); err != nil {
					log.Error("paste failed, the text is in the file", "path", out, "err", err)
					return
				}
				log.Info("pasted", "path", out)
			}
		}()
	}
}

func speechClient(cfg *config.Config) stt.Client {
	return stt.Client{APIKey: cfg.Groq.APIKey, Model: cfg.Groq.Model, Language: cfg.Groq.Language, Ignore: cfg.Groq.Ignore}
}

func firmwares(p paths) firmware.Store {
	return firmware.Store{Dir: filepath.Join(p.state, "firmware")}
}

func addFirmware(p paths, device, file, version string) error {
	states, err := store.Open(filepath.Join(p.state, "state.json"))
	if err != nil {
		return err
	}
	paired, ok := states.Device(device)
	if !ok {
		return fmt.Errorf("%s isn't paired", device)
	}
	info, err := firmwares(p).Add(device, board.Of(paired.Board), file, version)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%s) will get %s, %d bytes, on 5 presses.\n", device, info.Board, info.Version, info.Size)
	return nil
}

func install(p paths) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return err
	}
	if err := os.MkdirAll(p.state, 0o700); err != nil {
		return err
	}
	log := filepath.Join(p.state, "serve.log")
	if err := service.Install(executable, log); err != nil {
		return err
	}
	fmt.Printf("Running %s as %s, log in %s.\n", executable, service.Label, log)
	return nil
}

func transcribe(p paths, path string) error {
	cfg, err := config.Load(p.config)
	if err != nil {
		return err
	}
	if cfg.Groq.APIKey == "" {
		return errors.New("no groq api_key in the config")
	}
	ctx, cancel := context.WithTimeout(context.Background(), transcribeTimeout)
	defer cancel()
	start := time.Now()
	out, text, err := speechClient(cfg).TranscribeFile(ctx, path)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n(%s, %v)\n", text, out, time.Since(start).Round(time.Millisecond))
	return nil
}

func check(p paths) error {
	cfg, err := config.Load(p.config)
	if err != nil {
		return err
	}
	bound := 0
	for _, device := range cfg.Devices {
		bound += len(device.Gestures)
	}
	fmt.Printf("%s is valid: %d devices, %d gestures\n", p.config, len(cfg.Devices), bound)
	return nil
}

func listDevices(client *control.Client) error {
	devices, err := client.Devices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("No paired buttons yet, run `voistic pair`.")
		return nil
	}
	for _, device := range devices {
		seen := "never"
		if !device.LastSeen.IsZero() {
			seen = device.LastSeen.Local().Format(time.DateTime)
		}
		battery := "-"
		if device.Battery > 0 {
			battery = fmt.Sprintf("%.2f V", float64(device.Battery)/1000)
		}
		fmt.Printf("%-16s %-20s %-14s fw %-8s battery %-7s presses %-6s last seen %s\n",
			device.ID, device.Name, device.Board, device.Firmware, battery,
			strconv.FormatUint(device.Seq, 10), seen)
	}
	return nil
}

func test(client *control.Client, device, gesture string) error {
	result, err := client.Test(device, gesture)
	if err != nil {
		return err
	}
	if result.Output != "" {
		fmt.Println(result.Output)
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}
