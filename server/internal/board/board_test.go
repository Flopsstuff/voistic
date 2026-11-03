package board

import "testing"

func TestOldFirmwareMeansXiao(t *testing.T) {
	if got := Of(""); got != XiaoS3 {
		t.Fatalf("Of(\"\") = %q", got)
	}
	if got := Of(IoTButtonC6); got != IoTButtonC6 {
		t.Fatalf("Of(c6) = %q", got)
	}
}

func TestChips(t *testing.T) {
	for board, want := range map[string]uint16{"": chipS3, XiaoS3: chipS3, IoTButtonC6: chipC6} {
		if chip, ok := Chip(board); !ok || chip != want {
			t.Errorf("Chip(%q) = %d, %v", board, chip, ok)
		}
	}
	if _, ok := Chip("toaster"); ok {
		t.Error("an unknown board has a chip")
	}
}
