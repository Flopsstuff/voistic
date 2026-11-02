//go:build !darwin && !windows

package service

import "errors"

// Install isn't there yet on this system.
func Install(string, string) error {
	return errors.New("install only works on macOS and Windows for now")
}

// Uninstall isn't there yet on this system.
func Uninstall() error {
	return errors.New("uninstall only works on macOS and Windows for now")
}
