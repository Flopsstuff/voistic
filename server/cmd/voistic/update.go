package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"voistic/server/internal/board"
	"voistic/server/internal/release"
	"voistic/server/internal/service"
	"voistic/server/internal/store"
)

const downloadTimeout = 5 * time.Minute

// signIdentity is the certificate make build signs with, from
// scripts/make-signing-cert.sh. The macOS firewall and Accessibility tie their
// approval to it, so an update keeps them only if it's signed the same way.
const signIdentity = "Voistic Local"

// selfUpdate replaces this program with the latest release's, and restarts the
// server if it's installed.
func selfUpdate(checkOnly bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	latest, err := release.Latest(ctx)
	if err != nil {
		return err
	}
	if !release.Newer(latest.Tag, version) {
		fmt.Printf("voistic %s is up to date, the latest release is %s.\n", version, latest.Tag)
		return nil
	}
	if checkOnly {
		fmt.Printf("voistic %s is out, this is %s. voistic update installs it.\n", latest.Tag, version)
		return nil
	}

	name := release.ServerArchive(runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Downloading %s of %s...\n", name, latest.Tag)
	archive, err := latest.Fetch(ctx, name)
	if err != nil {
		return err
	}
	binaries, err := release.Binaries(archive, runtime.GOOS == "windows")
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return err
	}

	// On Windows the task runs voisticw.exe, next to voistic.exe.
	dir := filepath.Dir(executable)
	replaced := 0
	for file, data := range binaries {
		target := filepath.Join(dir, file)
		if target != executable {
			if _, err := os.Stat(target); err != nil {
				continue // not installed here
			}
		}
		if err := release.Replace(target, data); err != nil {
			return fmt.Errorf("can't replace %s: %w", target, err)
		}
		sign(target)
		replaced++
	}
	if replaced == 0 {
		return fmt.Errorf("%s has no %s", name, filepath.Base(executable))
	}
	fmt.Printf("Installed voistic %s in %s.\n", latest.Tag, dir)

	restarted, err := service.Restart()
	switch {
	case err != nil:
		return fmt.Errorf("installed, but can't restart the server: %w", err)
	case restarted:
		fmt.Println("Restarted the server.")
	default:
		fmt.Println("No installed server to restart. Restart voistic serve yourself if it runs.")
	}
	return nil
}

// sign signs a new binary on macOS with the certificate make build uses, if
// it's there, so the firewall keeps letting buttons through.
func sign(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	if exec.Command("security", "find-certificate", "-c", signIdentity).Run() != nil {
		fmt.Printf("No %q certificate, so the macOS firewall may ask about voistic again.\n", signIdentity)
		return
	}
	if output, err := exec.Command("codesign", "-f", "-s", signIdentity, "-i", "app.voistic.server", path).CombinedOutput(); err != nil {
		fmt.Printf("Can't sign %s: %v %s\n", path, err, output)
	}
}

// addLatestFirmware offers a paired button the latest release's firmware for
// its board. A XIAO takes the one with a microphone unless told otherwise.
func addLatestFirmware(p paths, device string, mic bool) error {
	states, err := store.Open(filepath.Join(p.state, "state.json"))
	if err != nil {
		return err
	}
	paired, ok := states.Device(device)
	if !ok {
		return fmt.Errorf("%s isn't paired", device)
	}
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	image, version, err := latestFirmware(ctx, board.Of(paired.Board), mic)
	if err != nil {
		return err
	}
	if version == paired.Firmware {
		return errors.New(device + " already runs " + version + ", the latest release")
	}
	path, err := writeTemp(image)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	return addFirmware(p, device, path, version)
}

// writeTemp puts a downloaded image in a temporary file for the firmware
// store. A short write, such as on a full disk, is an error here, rather than
// a cut image the button only refuses after the download.
func writeTemp(image []byte) (string, error) {
	tmp, err := os.CreateTemp("", "voistic-firmware-*.bin")
	if err != nil {
		return "", err
	}
	_, err = tmp.Write(image)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("can't keep the firmware: %w", err)
	}
	return tmp.Name(), nil
}
