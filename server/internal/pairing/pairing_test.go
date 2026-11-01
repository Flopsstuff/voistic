package pairing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitOpen(t *testing.T, w *Window) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); !w.IsOpen(); {
		if time.Now().After(deadline) {
			t.Fatal("window didn't open")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFirstClaimWins(t *testing.T) {
	var w Window
	result := make(chan string)
	go func() {
		device, _ := w.Wait(context.Background())
		result <- device
	}()
	waitOpen(t, &w)

	if !w.Claim("first") {
		t.Fatal("first claim refused")
	}
	if w.Claim("second") {
		t.Fatal("second claim accepted")
	}
	if device := <-result; device != "first" {
		t.Errorf("device = %q", device)
	}
}

func TestClaimOnClosedWindow(t *testing.T) {
	var w Window
	if w.Claim("a") {
		t.Fatal("claim accepted while closed")
	}
}

func TestTimeoutClosesWindow(t *testing.T) {
	var w Window
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := w.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if w.IsOpen() {
		t.Fatal("window still open")
	}
}

func TestOnlyOneWindow(t *testing.T) {
	var w Window
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Wait(ctx)
		close(done)
	}()
	waitOpen(t, &w)

	if _, err := w.Wait(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	cancel()
	<-done
}
