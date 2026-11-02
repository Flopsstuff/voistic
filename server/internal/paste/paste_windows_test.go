package paste

import (
	"testing"
	"unsafe"
)

func TestKeyInputHasTheSizeOfInput(t *testing.T) {
	if size := unsafe.Sizeof(keyInput{}); size != 40 {
		t.Errorf("size %d, SendInput rejects anything but sizeof(INPUT)", size)
	}
	if offset := unsafe.Offsetof(keyInput{}.vk); offset != 8 {
		t.Errorf("vk at %d, the union starts at 8", offset)
	}
}

func TestChordLetsGoInReverse(t *testing.T) {
	inputs := chord(vkControl, vkV)
	if len(inputs) != 4 {
		t.Fatalf("inputs %v", inputs)
	}
	want := []struct {
		vk    uint16
		flags uint32
	}{{vkControl, 0}, {vkV, 0}, {vkV, keyEventUp}, {vkControl, keyEventUp}}
	for i, w := range want {
		if inputs[i].vk != w.vk || inputs[i].flags != w.flags || inputs[i].kind != inputKeyboard {
			t.Errorf("input %d: %+v", i, inputs[i])
		}
	}
}

func TestEveryKeyHasAVirtualKey(t *testing.T) {
	for _, key := range Keys {
		if virtualKeys[key] == 0 {
			t.Errorf("no virtual key for %s", key)
		}
	}
}
