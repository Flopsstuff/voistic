#if defined(VOISTIC_MIC) && !defined(VOISTIC_BOARD_C6)

#include "mic.h"

#include <Arduino.h>
#include <driver/i2s_std.h>

namespace {

constexpr gpio_num_t kBclkPin = GPIO_NUM_44;  // D7
constexpr gpio_num_t kLrclPin = GPIO_NUM_8;   // D9
constexpr gpio_num_t kDataPin = GPIO_NUM_7;   // D8

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

i2s_chan_handle_t channel = nullptr;
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
    i2s_channel_read(channel, frames, sizeof(frames), &got, 100);
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
  i2s_channel_disable(channel);
  i2s_del_channel(channel);
  channel = nullptr;
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

  i2s_chan_config_t chan = I2S_CHANNEL_DEFAULT_CONFIG(I2S_NUM_0, I2S_ROLE_MASTER);
  chan.dma_desc_num = kDmaBuffers;
  chan.dma_frame_num = kDmaFrames;

  // Both slots, 32 bits each, as the legacy driver read them: the SPH0645 is on
  // the left (SEL on GND), and capture() takes every first of a pair.
  i2s_std_config_t config = {};
  config.clk_cfg = I2S_STD_CLK_DEFAULT_CONFIG(kCaptureRate);
  config.slot_cfg = I2S_STD_PHILIPS_SLOT_DEFAULT_CONFIG(I2S_DATA_BIT_WIDTH_32BIT, I2S_SLOT_MODE_STEREO);
  config.gpio_cfg.mclk = I2S_GPIO_UNUSED;
  config.gpio_cfg.bclk = kBclkPin;
  config.gpio_cfg.ws = kLrclPin;
  config.gpio_cfg.dout = I2S_GPIO_UNUSED;
  config.gpio_cfg.din = kDataPin;

  if (i2s_new_channel(&chan, nullptr, &channel) != ESP_OK ||
      i2s_channel_init_std_mode(channel, &config) != ESP_OK ||
      i2s_channel_enable(channel) != ESP_OK) {
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
