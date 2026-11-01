#ifdef VOISTIC_MIC

#include "mic.h"

#include <Arduino.h>
#include <driver/i2s.h>

namespace {

constexpr i2s_port_t kPort = I2S_NUM_0;
constexpr int kBclkPin = 44;  // D7
constexpr int kLrclPin = 8;   // D9
constexpr int kDataPin = 7;   // D8

// The SPH0645 doesn't run below 32 kHz, so it's halved to kMicRate here.
constexpr uint32_t kCaptureRate = kMicRate * 2;
constexpr int kDmaBuffers = 16;
constexpr int kDmaFrames = 256;
constexpr int kDataShift = 14;  // 18 bits at the top of a 32-bit slot
constexpr size_t kBufferBytes = 6 * 1024 * 1024;  // over three minutes

// The microphone sits far off zero, about 11% of full scale, and speech nearby
// peaks around -30 dBFS, so it needs both a DC filter and gain.
constexpr float kDcPole = 0.995f;
constexpr float kGain = 2.0f;  // from 18-bit values to 16-bit, which is +18 dB

// The microphone starts with a loud burst, so its first moments are dropped
// and what follows fades in.
constexpr uint32_t kDropSamples = kMicRate / 10;   // 0.1 s
constexpr uint32_t kFadeSamples = kMicRate / 5;    // 0.2 s

uint8_t* buffer = nullptr;
volatile size_t written = 0;  // total bytes, the position is modulo kBufferBytes
volatile size_t taken = 0;
volatile bool capturing = false;
bool started = false;

void put(int16_t sample) {
  if (written - taken + sizeof(sample) > kBufferBytes) {
    return;  // nobody is reading, and the oldest audio is worth more
  }
  memcpy(buffer + written % kBufferBytes, &sample, sizeof(sample));
  written = written + sizeof(sample);
}

void capture(void*) {
  static int32_t frames[kDmaFrames * 2];
  float lastInput = 0, lastOutput = 0, previous = 0;
  bool first = true, odd = false;
  uint32_t produced = 0;

  while (capturing) {
    size_t got = 0;
    i2s_read(kPort, frames, sizeof(frames), &got, pdMS_TO_TICKS(100));
    for (size_t i = 0; i + 1 < got / sizeof(int32_t); i += 2) {
      const float input = static_cast<float>(frames[i] >> kDataShift);
      if (first) {
        lastInput = input;
        first = false;
      }
      const float output = input - lastInput + kDcPole * lastOutput;
      lastInput = input;
      lastOutput = output;

      // Averaging each pair keeps the worst of the aliasing out.
      if (odd) {
        if (produced >= kDropSamples) {
          const uint32_t faded = produced - kDropSamples;
          const float fade = faded < kFadeSamples ? static_cast<float>(faded) / kFadeSamples : 1.0f;
          const float sample = (previous + output) / 2 * kGain * fade;
          put(static_cast<int16_t>(constrain(sample, -32768.0f, 32767.0f)));
        }
        ++produced;
      }
      previous = output;
      odd = !odd;
    }
  }
  i2s_driver_uninstall(kPort);
  vTaskDelete(nullptr);
}

}  // namespace

void micStart() {
  if (started) {
    return;
  }
  started = true;
  buffer = static_cast<uint8_t*>(ps_malloc(kBufferBytes));
  if (buffer == nullptr) {
    Serial.println("mic: no memory");
    return;
  }

  i2s_config_t config = {};
  config.mode = static_cast<i2s_mode_t>(I2S_MODE_MASTER | I2S_MODE_RX);
  config.sample_rate = kCaptureRate;
  config.bits_per_sample = I2S_BITS_PER_SAMPLE_32BIT;
  config.channel_format = I2S_CHANNEL_FMT_RIGHT_LEFT;  // SEL is on GND, so left
  config.communication_format = I2S_COMM_FORMAT_STAND_I2S;
  config.dma_buf_count = kDmaBuffers;
  config.dma_buf_len = kDmaFrames;

  i2s_pin_config_t pins = {};
  pins.mck_io_num = I2S_PIN_NO_CHANGE;
  pins.bck_io_num = kBclkPin;
  pins.ws_io_num = kLrclPin;
  pins.data_out_num = I2S_PIN_NO_CHANGE;
  pins.data_in_num = kDataPin;

  if (i2s_driver_install(kPort, &config, 0, nullptr) != ESP_OK ||
      i2s_set_pin(kPort, &pins) != ESP_OK) {
    Serial.println("mic: i2s failed");
    return;
  }
  capturing = true;
  xTaskCreatePinnedToCore(capture, "mic", 4096, nullptr, 5, nullptr, 1);  // Wi-Fi runs on core 0
}

size_t micRead(uint8_t* out, size_t maxBytes) {
  size_t count = min(maxBytes, static_cast<size_t>(written - taken));
  const size_t position = taken % kBufferBytes;
  count = min(count, kBufferBytes - position);
  if (count > 0) {
    memcpy(out, buffer + position, count);
    taken = taken + count;
  }
  return count;
}

void micStop() { capturing = false; }

#endif  // VOISTIC_MIC
