//go:build !darwin && !windows

package paste

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("the keyboard only works on macOS and Windows")

func pasteText(context.Context, string) error { return errUnsupported }

func pressKey(context.Context, string) error { return errUnsupported }

func pressBackspace(context.Context, int) error { return errUnsupported }
