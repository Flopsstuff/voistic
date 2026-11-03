// Package board names the hardware a button runs on, which decides the
// firmware it can take.
package board

const (
	// XiaoS3 is a Seeed XIAO ESP32S3 Plus with the Adafruit NeoPixel button.
	XiaoS3 = "xiao-s3"
	// IoTButtonC6 is a Seeed IoT Button V2, an ESP32-C6.
	IoTButtonC6 = "iot-button-c6"
)

// Chip IDs from the header of an ESP32 application image.
const (
	chipS3 = 9
	chipC6 = 13
)

// Of returns the board a button reported. Firmware from before boards were
// reported sends nothing, and every such button is a XIAO.
func Of(reported string) string {
	if reported == "" {
		return XiaoS3
	}
	return reported
}

// Chip returns the chip ID an image for the board must carry, and false for a
// board this server doesn't know.
func Chip(board string) (uint16, bool) {
	switch Of(board) {
	case XiaoS3:
		return chipS3, true
	case IoTButtonC6:
		return chipC6, true
	}
	return 0, false
}

// ChipName is for messages.
func ChipName(chip uint16) string {
	switch chip {
	case chipS3:
		return "ESP32-S3"
	case chipC6:
		return "ESP32-C6"
	}
	return "unknown chip"
}
