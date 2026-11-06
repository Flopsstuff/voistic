#if defined(VOISTIC_MIC) && defined(VOISTIC_BOARD_C6)

// The analog DFRobot SEN0487 on the IoT Button V2's test pads:
//
//   pad 5 GPIO7  -> VCC, powered from the pin, so it's off in deep sleep
//   pad 4 GND    -> GND
//   pad 3 GPIO6  -> A, ADC1 channel 6, idles at 1.5 V
//
// ADC1 runs in continuous mode at 4 times the rate, and each 4 samples are
// summed, which filters a little and gives 14-bit values. There is no PSRAM, so
// the buffer covers a few seconds, which is what the network needs to come up.
// See research/iot-button-c6/NOTES.md for the measurements.

#include "mic.h"

#include <Arduino.h>
#include <esp_adc/adc_continuous.h>

namespace {

constexpr uint8_t kPowerPin = 7;
constexpr adc_channel_t kChannel = ADC_CHANNEL_6;
constexpr uint32_t kOversample = 4;
constexpr uint32_t kAdcRate = kMicRate * kOversample;
constexpr size_t kFrameBytes = 1024;
constexpr size_t kBufferBytes = 128 * 1024;  // 4 s

// A sum of 4 12-bit values is 14 bits, so x4 brings it to 16. Speech at hand's
// length then peaks around -10 dBFS.
constexpr float kGain = 4.0f;
constexpr float kDcPole = 0.995f;

// The output swings while the module powers up, so its first moments are
// dropped and what follows fades in.
constexpr uint32_t kDropSamples = kMicRate / 10;  // 0.1 s
constexpr uint32_t kFadeSamples = kMicRate / 5;   // 0.2 s

adc_continuous_handle_t adc = nullptr;
uint8_t* buffer = nullptr;
volatile size_t written = 0;  // total bytes, the position is modulo kBufferBytes
volatile size_t taken = 0;
volatile bool capturing = false;
volatile bool running = false;  // the capture task exists

void put(int16_t sample) {
  if (written - taken + sizeof(sample) > kBufferBytes) {
    return;  // nobody is reading, and the oldest audio is worth more
  }
  memcpy(buffer + written % kBufferBytes, &sample, sizeof(sample));
  written = written + sizeof(sample);
}

bool startAdc();

// Starts and stops the ADC itself: the driver holds a lock from start to stop,
// and releasing it from another task trips a FreeRTOS assert.
void capture(void*) {
  if (!startAdc()) {
    Serial.println("mic: adc failed");
    capturing = false;
    digitalWrite(kPowerPin, LOW);
    running = false;
    vTaskDelete(nullptr);
  }
  static uint8_t frame[kFrameBytes];
  float lastInput = 0, lastOutput = 0;
  bool first = true;
  uint32_t sum = 0, inSum = 0, produced = 0;

  while (capturing) {
    uint32_t got = 0;
    if (adc_continuous_read(adc, frame, sizeof(frame), &got, 100) != ESP_OK) {
      continue;
    }
    for (uint32_t i = 0; i + SOC_ADC_DIGI_RESULT_BYTES <= got; i += SOC_ADC_DIGI_RESULT_BYTES) {
      const auto* data = reinterpret_cast<const adc_digi_output_data_t*>(&frame[i]);
      if (data->type2.channel != kChannel) {
        continue;
      }
      sum += data->type2.data;
      if (++inSum < kOversample) {
        continue;
      }
      const float input = static_cast<float>(sum);
      sum = 0;
      inSum = 0;
      if (first) {
        lastInput = input;
        first = false;
      }
      const float output = input - lastInput + kDcPole * lastOutput;
      lastInput = input;
      lastOutput = output;

      if (produced >= kDropSamples) {
        const uint32_t faded = produced - kDropSamples;
        const float fade = faded < kFadeSamples ? static_cast<float>(faded) / kFadeSamples : 1.0f;
        put(static_cast<int16_t>(constrain(output * kGain * fade, -32768.0f, 32767.0f)));
      }
      ++produced;
    }
  }
  adc_continuous_stop(adc);
  adc_continuous_deinit(adc);
  adc = nullptr;
  digitalWrite(kPowerPin, LOW);
  running = false;
  vTaskDelete(nullptr);
}

bool startAdc() {
  adc_continuous_handle_cfg_t handle = {};
  handle.max_store_buf_size = 4 * kFrameBytes;
  handle.conv_frame_size = kFrameBytes;
  if (adc_continuous_new_handle(&handle, &adc) != ESP_OK) {
    return false;
  }
  adc_digi_pattern_config_t pattern = {};
  pattern.atten = ADC_ATTEN_DB_12;
  pattern.channel = kChannel;
  pattern.unit = ADC_UNIT_1;
  pattern.bit_width = SOC_ADC_DIGI_MAX_BITWIDTH;

  adc_continuous_config_t config = {};
  config.pattern_num = 1;
  config.adc_pattern = &pattern;
  config.sample_freq_hz = kAdcRate;
  config.conv_mode = ADC_CONV_SINGLE_UNIT_1;
  config.format = ADC_DIGI_OUTPUT_FORMAT_TYPE2;
  return adc_continuous_config(adc, &config) == ESP_OK && adc_continuous_start(adc) == ESP_OK;
}

}  // namespace

// Can run again after micStop, for the next press of a board that stays
// awake. That drops whatever the last recording left unread.
void micStart() {
  if (capturing) {
    return;
  }
  for (int i = 0; running && i < 150; ++i) {
    delay(2);  // the last capture is still closing the ADC, a read takes up to 100 ms
  }
  if (running) {
    Serial.println("mic: still stopping");
    return;
  }
  if (buffer == nullptr) {
    buffer = static_cast<uint8_t*>(malloc(kBufferBytes));
  }
  if (buffer == nullptr) {
    Serial.println("mic: no memory");
    return;
  }
  written = 0;
  taken = 0;
  pinMode(kPowerPin, OUTPUT);
  digitalWrite(kPowerPin, HIGH);
  capturing = true;
  running = true;
  // Above the main loop, so a slow HTTP write doesn't starve the ADC.
  xTaskCreate(capture, "mic", 4096, nullptr, 5, nullptr);
}

size_t micRead(uint8_t* out, size_t maxBytes) {
  if (buffer == nullptr) {
    return 0;
  }
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

void micDiscard() { taken = written; }

bool micIdle() { return !running; }

#endif  // VOISTIC_MIC && VOISTIC_BOARD_C6
