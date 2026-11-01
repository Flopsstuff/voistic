#include "leds.h"

#include <Adafruit_NeoPixel.h>
#include <Arduino.h>

namespace {

constexpr uint8_t kPixelPin = A3;  // GPIO4
constexpr uint32_t kFrameMs = 10;

constexpr float kDefaultBrightness = 0.2f;  // until `brightness` is set over USB

enum class Shape : uint8_t { Plain, Fades, Breathes };

struct Pattern {
  uint32_t color;
  uint16_t onMs;
  uint16_t offMs;
  uint8_t repeats;  // 0 means until another state is set
  Shape shape;
};

// Every state's look, in the order of the Led enum. A press in progress blinks,
// and its colour moves around the hue wheel from light blue: towards green for
// more presses, towards violet for a hold. Green means done, red means failed.
constexpr Pattern kPatterns[] = {
    /* Off         */ {0x000000, 0, 0, 0, Shape::Plain},
    /* Press1      */ {0x002850, 50, 50, 0, Shape::Plain},    // fast light blue
    /* Press2      */ {0x005028, 50, 50, 0, Shape::Plain},    // fast sea green
    /* Press3      */ {0x285000, 50, 50, 0, Shape::Plain},    // fast lime
    /* Press4      */ {0x505000, 50, 50, 0, Shape::Plain},    // fast yellow
    /* HoldWaiting */ {0x000050, 50, 50, 0, Shape::Plain},    // fast blue
    /* HoldLong    */ {0x280050, 1000, 0, 0, Shape::Plain},   // steady violet
    /* HoldArmed   */ {0x080010, 1000, 0, 0, Shape::Plain},   // steady dim purple
    /* HoldPair    */ {0x280040, 1000, 0, 0, Shape::Plain},   // steady purple
    /* HoldReset   */ {0x400000, 150, 150, 0, Shape::Plain},  // fast red
    /* Accepted    */ {0x005000, 1000, 0, 1, Shape::Fades},    // green, fading out
    /* Failed      */ {0x500000, 1000, 0, 1, Shape::Fades},    // red, fading out
    /* Updating    */ {0x000080, 2000, 0, 0, Shape::Breathes},  // blue, slowly in and out
    /* Portal      */ {0x202020, 500, 500, 0, Shape::Plain},  // slow white
    /* Pairing     */ {0x180028, 500, 500, 0, Shape::Plain},  // slow purple
    /* Paired      */ {0x004000, 150, 150, 3, Shape::Plain},  // three green
};

Adafruit_NeoPixel pixel(1, kPixelPin, NEO_GRB + NEO_KHZ800);

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
  for (;;) {
    const uint32_t now = millis();
    const Pattern& pattern = kPatterns[static_cast<size_t>(current)];
    const uint32_t period = pattern.onMs + pattern.offMs;
    const uint32_t elapsed = now - startedAtMs;

    uint32_t color = 0;
    if (period > 0) {
      if (pattern.repeats != 0 && elapsed >= period * pattern.repeats) {
        finished = true;
      } else if (pattern.shape == Shape::Breathes) {
        const float wave = (1.0f - cosf(TWO_PI * (elapsed % period) / period)) / 2;
        color = scaled(pattern.color, static_cast<uint32_t>(wave * 256), 256);
      } else if (elapsed % period < pattern.onMs) {
        const bool fades = pattern.shape == Shape::Fades;
        color = fades ? scaled(pattern.color, period - elapsed, period) : pattern.color;
      }
    }
    if (halved) {
      color = scaled(color, 1, 2);
    }
    color = scaled(color, static_cast<uint32_t>(brightness * 256), 256);

    if (color != shown) {
      shown = color;
      pixel.setPixelColor(0, color);
      pixel.show();
    }
    vTaskDelay(pdMS_TO_TICKS(kFrameMs));
  }
}

}  // namespace

void ledsBegin() {
  pixel.begin();
  pixel.clear();
  pixel.show();
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

void ledsWait() {
  while (!finished) {
    delay(kFrameMs);
  }
}

void ledsStop() {
  vTaskDelete(task);
  pixel.clear();
  pixel.show();
}
