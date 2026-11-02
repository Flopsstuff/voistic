package paste

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func record(t *testing.T, fail string) *[]string {
	t.Helper()
	var calls []string
	original := run
	t.Cleanup(func() { run = original })
	run = func(_ context.Context, stdin string, name string, args ...string) error {
		calls = append(calls, name+"|"+stdin+"|"+strings.Join(args, " "))
		if name == fail {
			return errors.New("refused")
		}
		return nil
	}
	return &calls
}

func TestTextCopiesThenPresses(t *testing.T) {
	calls := record(t, "")
	if err := Text(context.Background(), `кавычка " и ) "; do shell script`); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !strings.HasPrefix((*calls)[0], "pbcopy|кавычка") {
		t.Fatalf("calls %q", *calls)
	}
	if strings.Contains((*calls)[1], "кавычка") {
		t.Errorf("the text reached AppleScript: %q", (*calls)[1])
	}
}

func TestNothingIsPressedIfCopyFails(t *testing.T) {
	calls := record(t, "pbcopy")
	if err := Text(context.Background(), "текст"); err == nil {
		t.Fatal("no error")
	}
	if len(*calls) != 1 {
		t.Errorf("calls %q", *calls)
	}
}

func TestEmptyTextDoesNothing(t *testing.T) {
	calls := record(t, "")
	if err := Text(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Errorf("calls %q", *calls)
	}
}

func TestKeyPressesItsCode(t *testing.T) {
	calls := record(t, "")
	if err := Key(context.Background(), "tab"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "key code 48") {
		t.Fatalf("calls %q", *calls)
	}
}

func TestEveryKeyHasACode(t *testing.T) {
	for _, key := range Keys {
		if _, ok := keyCodes[key]; !ok {
			t.Errorf("no code for %s", key)
		}
	}
}
