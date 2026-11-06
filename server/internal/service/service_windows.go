package service

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// background is the build with no console window, next to voistic.exe.
const background = "voisticw.exe"

// Install registers the task and starts it, replacing an earlier one.
func Install(executable, log string) error {
	program := filepath.Join(filepath.Dir(executable), background)
	if _, err := os.Stat(program); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s has to be next to %s", background, filepath.Base(executable))
		}
		return err
	}
	current, err := user.Current()
	if err != nil {
		return err
	}
	definition, err := os.CreateTemp("", "voistic-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(definition.Name())
	_, err = definition.Write(Task(program, log, current.Username))
	if closed := definition.Close(); err == nil {
		err = closed
	}
	if err != nil {
		return err
	}

	exec.Command("schtasks", "/End", "/TN", Label).Run() // fine if it wasn't running
	if err := schtasks("/Create", "/TN", Label, "/XML", definition.Name(), "/F"); err != nil {
		return err
	}
	return schtasks("/Run", "/TN", Label)
}

// Uninstall stops the task and removes it.
func Uninstall() error {
	if exec.Command("schtasks", "/Query", "/TN", Label).Run() != nil {
		return nil // nothing to remove
	}
	exec.Command("schtasks", "/End", "/TN", Label).Run() // fine if it wasn't running
	return schtasks("/Delete", "/TN", Label, "/F")
}

func schtasks(args ...string) error {
	output, err := exec.Command("schtasks", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks %s: %w: %s", args[0], err, bytes.TrimSpace(output))
	}
	return nil
}

// Restart restarts the task, if it's installed, so it runs a new binary. It
// reports whether there was one.
func Restart() (bool, error) {
	if exec.Command("schtasks", "/Query", "/TN", Label).Run() != nil {
		return false, nil
	}
	exec.Command("schtasks", "/End", "/TN", Label).Run() // fine if it wasn't running
	return true, schtasks("/Run", "/TN", Label)
}
