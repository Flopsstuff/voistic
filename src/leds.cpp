#include "leds.h"

#include <Arduino.h>

#ifdef VOISTIC_BOARD_C6
// The RGB LED is driven by rgbLedWrite from the core, no library needed.
#else
#include <Adafruit_NeoPixel.h>
#endif

namespace {

constexpr uint32_t kFrameMs = 10;

constexpr float kDefaultBrightness = 0.2f;  // until `brightness` is set over USB

enum class Shape : uint8_t { Plain, Fades, Breathes };

// The red/blue LED beside the RGB LED, on the IoT Button V2 only.
enum class Side : uint8_t {
  None,
  BlueBlink,  // fast blue, on its own clock: the network isn't there yet
  Siren,      // blue in the on part of the period, red in the off part
  Red,        // red in the on part of the period
};

constexpr uint32_t kBlueBlinkMs = 50;

struct Pattern {
  uint32_t color;
  uint16_t onMs;
  uint16_t offMs;
  uint8_t repeats;  // 0 means until another state is set
  Shape shape;
  Side side;
};

// Every state's look, in the order of the Led enum. A press in progress blinks,
// and its colour moves around the hue wheel from light blue: towards green for
// more presses, towards violet for a hold. Green means done, red means failed.
constexpr Pattern kPatterns[] = {
    /* Off         */ {0x000000, 0, 0, 0, Shape::Plain, Side::None},
    /* Press1      */ {0x002850, 50, 50, 0, Shape::Plain, Side::None},    // fast light blue
    /* Press2      */ {0x005028, 50, 50, 0, Shape::Plain, Side::None},    // fast sea green
    /* Press3      */ {0x285000, 50, 50, 0, Shape::Plain, Side::None},    // fast lime
    /* Press4      */ {0x505000, 50, 50, 0, Shape::Plain, Side::None},    // fast yellow
#ifdef VOISTIC_BOARD_C6
    // Violet at once, since the microphone records from the wake. The wait for
    // the network moves to the blue LED beside it.
    /* HoldWaiting */ {0x280050, 1000, 0, 0, Shape::Plain, Side::BlueBlink},
#else
    /* HoldWaiting */ {0x000050, 50, 50, 0, Shape::Plain, Side::None},    // fast blue
#endif
    /* HoldLong    */ {0x280050, 1000, 0, 0, Shape::Plain, Side::None},   // steady violet
    /* HoldArmed   */ {0x080010, 1000, 0, 0, Shape::Plain, Side::None},   // steady dim purple
    /* HoldPair    */ {0x280040, 1000, 0, 0, Shape::Plain, Side::None},   // steady purple
    /* HoldReset   */ {0x400000, 150, 150, 0, Shape::Plain, Side::None},  // fast red
    /* Accepted    */ {0x005000, 1000, 0, 1, Shape::Fades, Side::None},    // green, fading out
    /* Failed      */ {0x500000, 1000, 0, 1, Shape::Fades, Side::None},    // red, fading out
    /* Updating    */ {0x000080, 2000, 0, 0, Shape::Breathes, Side::None},  // blue, in and out
    /* Portal      */ {0x202020, 500, 500, 0, Shape::Plain, Side::None},  // slow white
#ifdef VOISTIC_BOARD_C6
    /* Pairing     */ {0x000000, 250, 250, 0, Shape::Plain, Side::Siren},  // blue-red siren
#else
    /* Pairing     */ {0x180028, 500, 500, 0, Shape::Plain, Side::None},  // slow purple
#endif
    /* Paired      */ {0x004000, 150, 150, 3, Shape::Plain, Side::None},  // three green
    /* BatteryLow  */ {0x000000, 150, 150, 2, Shape::Plain, Side::Red},   // two red beside
};

static_assert(sizeof(kPatterns) / sizeof(kPatterns[0]) == static_cast<size_t>(Led::BatteryLow) + 1,
              "a pattern for every state");

#ifdef VOISTIC_BOARD_C6

constexpr uint8_t kRgbDataPin = 19;
constexpr uint8_t kRgbPowerPin = 18;  // load switch, on when high
constexpr uint8_t kRedPin = 14;       // on when low
constexpr uint8_t kBluePin = 3;       // on when low

void outputBegin() {
  pinMode(kRedPin, OUTPUT);
  pinMode(kBluePin, OUTPUT);
  digitalWrite(kRedPin, HIGH);
  digitalWrite(kBluePin, HIGH);
  pinMode(kRgbPowerPin, OUTPUT);
  digitalWrite(kRgbPowerPin, HIGH);
  delay(1);  // the LED takes its first frame once powered
  rgbLedWrite(kRgbDataPin, 0, 0, 0);
}

void showColor(uint32_t color) {
  rgbLedWrite(kRgbDataPin, (color >> 16) & 0xFF, (color >> 8) & 0xFF, color & 0xFF);
}

void showSide(bool red, bool blue) {
  digitalWrite(kRedPin, red ? LOW : HIGH);
  digitalWrite(kBluePin, blue ? LOW : HIGH);
}

void outputStop() {
  showColor(0);
  showSide(false, false);
  digitalWrite(kRgbPowerPin, LOW);
}

#else

constexpr uint8_t kPixelPin = A3;  // GPIO4
Adafruit_NeoPixel pixel(1, kPixelPin, NEO_GRB + NEO_KHZ800);

void outputBegin() {
  pixel.begin();
  pixel.clear();
  pixel.show();
}

void showColor(uint32_t color) {
  pixel.setPixelColor(0, color);
  pixel.show();
}

void showSide(bool, bool) {}

void outputStop() {
  pixel.clear();
  pixel.show();
}

#endif

TaskHandle_t task = nullptr;
volatile Led current = Led::Off;
volatile uint32_t startedAtMs = 0;
volatile bool halved = false;
volatile float brightness = kDefaultBrightness;
volatile bool finished = true;

uint32_t scaled(uint32_t color, uint32_t part, uint32_t whole) {
  uint32_t result = 0;
  for (int shift = 0; shift <= 16; shift += 8) {
    result |= (((color >> shift) & 0xFF) * part / whole) << shift;
  }
  return result;
}

void render(void*) {
  uint32_t shown = 0xFFFFFFFF;
  uint8_t shownSide = 0xFF;
  for (;;) {
    const uint32_t now = millis();
    const Pattern& pattern = kPatterns[static_cast<size_t>(current)];
    const uint32_t period = pattern.onMs + pattern.offMs;
    const uint32_t elapsed = now - startedAtMs;

    uint32_t color = 0;
    bool red = false;
    bool blue = false;
    if (period > 0) {
      const bool on = elapsed % period < pattern.onMs;
      if (pattern.repeats != 0 && elapsed >= period * pattern.repeats) {
        finished = true;
      } else {
        if (pattern.shape == Shape::Breathes) {
          const float wave = (1.0f - cosf(TWO_PI * (elapsed % period) / period)) / 2;
          color = scaled(pattern.color, static_cast<uint32_t>(wave * 256), 256);
        } else if (on) {
          const bool fades = pattern.shape == Shape::Fades;
          color = fades ? scaled(pattern.color, period - elapsed, period) : pattern.color;
        }
        switch (pattern.side) {
          case Side::BlueBlink:
            blue = elapsed % (2 * kBlueBlinkMs) < kBlueBlinkMs;
            break;
          case Side::Siren:
            blue = on;
            red = !on;
            break;
          case Side::Red:
            red = on;
            break;
          case Side::None:
            break;
        }
      }
    }
    if (halved) {
      color = scaled(color, 1, 2);
    }
    color = scaled(color, static_cast<uint32_t>(brightness * 256), 256);

    if (color != shown) {
      shown = color;
      showColor(color);
    }
    const uint8_t side = (red ? 1 : 0) | (blue ? 2 : 0);
    if (side != shownSide) {
      shownSide = side;
      showSide(red, blue);
    }
    vTaskDelay(pdMS_TO_TICKS(kFrameMs));
  }
}

}  // namespace

void ledsBegin() {
  outputBegin();
  xTaskCreate(render, "leds", 2048, nullptr, 2, &task);
}

void ledsSet(Led led, bool half) {
  halved = half;
  if (led == current) {
    return;
  }
  finished = kPatterns[static_cast<size_t>(led)].repeats == 0;
  startedAtMs = millis();
  current = led;
}

void ledsSetBrightness(float value) { brightness = value; }

float ledsBrightness() { return brightness; }

bool ledsDone() { return finished; }

void ledsWait() {
  while (!finished) {
    delay(kFrameMs);
  }
}

void ledsStop() {
  vTaskDelete(task);
  outputStop();
}
