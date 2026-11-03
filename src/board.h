// What differs between the boards a button can be built on: pins, waking from
// deep sleep, and what's left on while asleep. The env picks one, see
// platformio.ini.
//
//   default         Seeed XIAO ESP32S3 Plus with the Adafruit NeoPixel button
//   VOISTIC_BOARD_C6  Seeed IoT Button V2, an ESP32-C6, see research/iot-button-c6

#pragma once

#include <Arduino.h>

#ifdef VOISTIC_BOARD_C6
#define VOISTIC_BOARD "iot-button-c6"
constexpr uint8_t kButtonPin = 2;  // K1, shorts to GND, 10k pull-up on the board
// Its 18650 can afford to wait for the next press in light sleep with Wi-Fi
// connected, which skips the boot and the join.
constexpr bool kBoardStaysConnected = true;
#else
#define VOISTIC_BOARD "xiao-s3"
constexpr uint8_t kButtonPin = A2;  // GPIO3, shorts to GND when pressed
constexpr bool kBoardStaysConnected = false;
#endif

// Without a word from the server, a board that stays connected does so this long.
constexpr uint32_t kDefaultStayConnectedMs = 30 * 60 * 1000;

// Sets up what the board needs at boot, before anything else.
void boardBegin();

// Whether the button woke the board from deep sleep.
bool boardWokenByButton();

// Battery voltage in mV, measured at boot, or 0 where the board can't measure
// it or no battery is connected.
int boardBatteryMv();

// Measures the battery again, for a press that didn't start with a boot. Not
// while the microphone runs, since both use the ADC.
void boardMeasureBattery();

// Below this, a button warns on every wake.
constexpr int kBatteryLowMv = 3500;

// Lets the chip light-sleep between Wi-Fi beacons, or keeps it fully awake.
// Only where kBoardStaysConnected.
void boardAllowLightSleep(bool allow);

// Turns off what would drain the battery, arms the button and sleeps.
[[noreturn]] void boardSleep();
