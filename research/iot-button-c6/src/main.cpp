// Analog microphone test (DFRobot SEN0487 on the test pads).
//
//   pad 5 GPIO7  -> mic VCC, powered from the pin so it can be off in sleep
//   pad 4 GND    -> mic GND
//   pad 3 GPIO6  -> mic A, ADC1 channel 6
//
// Hold the main button to record, up to 6 s, with the red LED on. The ADC
// runs in continuous mode at 64 kHz and every 4 samples are summed, which
// gives 16 kHz with 14-bit values. The recording stays in RAM.
//
// Wi-Fi joins the network saved in NVS, with modem sleep off as in voistic.
// While recording, and when asked, a task sends UDP to the gateway at about
// the rate voistic streams audio, so the radio is busy as in real use.
//
// Commands over USB, one per line:
//   d                     send the recording
//   s                     idle statistics, radio quiet
//   S                     idle statistics, radio sending
//   t                     toggle sending while recording (on by default)
//   wifi <ssid>|<pass>    save a network and join it

#include <Arduino.h>
#include <Preferences.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <esp_adc/adc_cali.h>
#include <esp_adc/adc_cali_scheme.h>
#include <esp_adc/adc_continuous.h>

namespace {

constexpr uint16_t kTrafficPort = 9;      // discard
constexpr size_t kTrafficPacket = 1024;
constexpr uint32_t kTrafficEveryMs = 30;  // about 34 KB/s, voistic sends 32 KB/s

volatile bool sending = false;
bool sendWhileRecording = true;
uint32_t packetsSent = 0;

void traffic(void*) {
  static uint8_t payload[kTrafficPacket];
  WiFiUDP udp;
  for (;;) {
    if (sending && WiFi.status() == WL_CONNECTED) {
      udp.beginPacket(WiFi.gatewayIP(), kTrafficPort);
      udp.write(payload, sizeof(payload));
      if (udp.endPacket()) {
        ++packetsSent;
      }
    }
    delay(kTrafficEveryMs);
  }
}

void joinSaved() {
  Preferences prefs;
  prefs.begin("wifi", /*readOnly=*/true);
  const String ssid = prefs.getString("ssid");
  const String pass = prefs.getString("pass");
  prefs.end();
  if (ssid.isEmpty()) {
    Serial.println("wifi: no network saved");
    return;
  }
  WiFi.setSleep(false);
  WiFi.mode(WIFI_STA);
  WiFi.begin(ssid.c_str(), pass.c_str());
  Serial.printf("wifi: joining %s\n", ssid.c_str());
}

void saveNetwork(const String& line) {
  const int bar = line.indexOf('|');
  if (bar < 0) {
    Serial.println("wifi: expected wifi <ssid>|<pass>");
    return;
  }
  Preferences prefs;
  prefs.begin("wifi", /*readOnly=*/false);
  prefs.putString("ssid", line.substring(5, bar));
  prefs.putString("pass", line.substring(bar + 1));
  prefs.end();
  WiFi.disconnect();
  joinSaved();
}

constexpr int kMicPower = 7;
constexpr int kButton = 2;    // to GND, pulled up on the board
constexpr int kLedRed = 14;   // on when LOW
constexpr int kLedBlue = 3;   // on when LOW
constexpr int kRgbPower = 18;

constexpr adc_channel_t kChannel = ADC_CHANNEL_6;
constexpr adc_atten_t kAtten = ADC_ATTEN_DB_12;
constexpr uint32_t kAdcRate = 64000;
constexpr uint32_t kOversample = 4;
constexpr uint32_t kRate = kAdcRate / kOversample;
constexpr uint32_t kMaxSamples = kRate * 6;

adc_continuous_handle_t adc = nullptr;
adc_cali_handle_t cali = nullptr;
uint16_t* samples = nullptr;
uint32_t recorded = 0;
uint32_t measuredRate = 0;

void adcSetup() {
  adc_continuous_handle_cfg_t handle = {};
  handle.max_store_buf_size = 8192;
  handle.conv_frame_size = 1024;
  ESP_ERROR_CHECK(adc_continuous_new_handle(&handle, &adc));

  adc_digi_pattern_config_t pattern = {};
  pattern.atten = kAtten;
  pattern.channel = kChannel;
  pattern.unit = ADC_UNIT_1;
  pattern.bit_width = SOC_ADC_DIGI_MAX_BITWIDTH;

  adc_continuous_config_t config = {};
  config.pattern_num = 1;
  config.adc_pattern = &pattern;
  config.sample_freq_hz = kAdcRate;
  config.conv_mode = ADC_CONV_SINGLE_UNIT_1;
  config.format = ADC_DIGI_OUTPUT_FORMAT_TYPE2;
  ESP_ERROR_CHECK(adc_continuous_config(adc, &config));

  adc_cali_curve_fitting_config_t calibration = {};
  calibration.unit_id = ADC_UNIT_1;
  calibration.chan = kChannel;
  calibration.atten = kAtten;
  calibration.bitwidth = ADC_BITWIDTH_12;
  if (adc_cali_create_scheme_curve_fitting(&calibration, &cali) != ESP_OK) {
    cali = nullptr;
    Serial.println("no ADC calibration, mV will be missing");
  }
}

// Fills out with sums of kOversample samples until maxOut or until keepGoing
// says stop, and returns the count.
uint32_t capture(uint16_t* out, uint32_t maxOut, bool (*keepGoing)()) {
  static uint8_t frame[1024];
  uint32_t count = 0, sum = 0, inSum = 0;
  ESP_ERROR_CHECK(adc_continuous_start(adc));
  const int64_t startedUs = esp_timer_get_time();
  while (count < maxOut && keepGoing()) {
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
      if (++inSum == kOversample) {
        if (count < maxOut) {
          out[count++] = sum;
        }
        sum = 0;
        inSum = 0;
      }
    }
  }
  const int64_t elapsedUs = esp_timer_get_time() - startedUs;
  ESP_ERROR_CHECK(adc_continuous_stop(adc));
  measuredRate = elapsedUs > 0 ? static_cast<uint32_t>(count * 1000000LL / elapsedUs) : 0;
  return count;
}

void printStats(const char* label, const uint16_t* data, uint32_t count) {
  if (count == 0) {
    Serial.printf("%s: no samples\n", label);
    return;
  }
  uint64_t total = 0;
  uint16_t low = 0xffff, high = 0;
  for (uint32_t i = 0; i < count; ++i) {
    total += data[i];
    low = min(low, data[i]);
    high = max(high, data[i]);
  }
  const float mean = static_cast<float>(total) / count;
  double square = 0;
  for (uint32_t i = 0; i < count; ++i) {
    const double d = data[i] - mean;
    square += d * d;
  }
  // In 12-bit ADC units, since each value is a sum of kOversample.
  const float mean12 = mean / kOversample;
  int millivolts = -1;
  if (cali != nullptr) {
    adc_cali_raw_to_voltage(cali, static_cast<int>(mean12 + 0.5f), &millivolts);
  }
  Serial.printf("%s: %lu samples, rate %lu Hz, mean %.1f (12-bit) = %d mV, "
                "min %.1f max %.1f, rms %.2f (12-bit)\n",
                label, static_cast<unsigned long>(count), static_cast<unsigned long>(measuredRate),
                mean12, millivolts, static_cast<float>(low) / kOversample,
                static_cast<float>(high) / kOversample, sqrt(square / count) / kOversample);
}

bool always() { return true; }
bool buttonHeld() { return digitalRead(kButton) == LOW; }

void idleStats(bool withTraffic) {
  sending = withTraffic;
  const uint32_t before = packetsSent;
  const uint32_t count = capture(samples, kRate / 2, always);
  sending = false;
  printStats(withTraffic ? "idle 0.5 s, sending" : "idle 0.5 s", samples, count);
  if (withTraffic) {
    Serial.printf("  %lu packets sent, rssi %d\n", static_cast<unsigned long>(packetsSent - before),
                  WiFi.RSSI());
  }
  recorded = 0;
}

void dump() {
  if (recorded == 0) {
    Serial.println("nothing recorded");
    return;
  }
  Serial.setTxTimeoutMs(1000);  // the computer is reading now
  Serial.printf("DATA %lu %lu %lu\n", static_cast<unsigned long>(recorded),
                static_cast<unsigned long>(measuredRate), static_cast<unsigned long>(kOversample));
  Serial.write(reinterpret_cast<const uint8_t*>(samples), recorded * sizeof(uint16_t));
  Serial.println();
  Serial.println("END");
  Serial.flush();
  Serial.setTxTimeoutMs(10);
}

}  // namespace

void setup() {
  Serial.setTxBufferSize(4096);
  Serial.begin(115200);
  // With nobody reading USB, a print waits out this timeout once the buffer
  // fills. Not 0, which hangs the board.
  Serial.setTxTimeoutMs(10);

  pinMode(kLedRed, OUTPUT);
  pinMode(kLedBlue, OUTPUT);
  digitalWrite(kLedRed, HIGH);
  digitalWrite(kLedBlue, HIGH);
  pinMode(kRgbPower, OUTPUT);
  digitalWrite(kRgbPower, LOW);  // RGB LED unpowered, it isn't used here
  pinMode(kButton, INPUT);

  pinMode(kMicPower, OUTPUT);
  digitalWrite(kMicPower, HIGH);

  samples = static_cast<uint16_t*>(malloc(kMaxSamples * sizeof(uint16_t)));
  if (samples == nullptr) {
    Serial.println("no memory for the recording");
  }
  adcSetup();
  xTaskCreate(traffic, "traffic", 4096, nullptr, 1, nullptr);
  joinSaved();  // after the recording buffer, so Wi-Fi can't take its memory
  delay(500);   // let the microphone output settle
  Serial.printf("Mic test ready: hold the button to record, free heap %lu\n",
                static_cast<unsigned long>(ESP.getFreeHeap()));
}

void loop() {
  static uint32_t lastStatsMs = 0;
  if (samples == nullptr) {
    delay(1000);
    Serial.println("no memory for the recording");
    return;
  }

  if (buttonHeld()) {
    digitalWrite(kLedRed, LOW);
    const uint32_t before = packetsSent;
    sending = sendWhileRecording;
    recorded = capture(samples, kMaxSamples, buttonHeld);
    sending = false;
    digitalWrite(kLedRed, HIGH);
    printStats("recording", samples, recorded);
    Serial.printf("  wifi %s, %lu packets sent during it, rssi %d\n",
                  WiFi.status() == WL_CONNECTED ? "connected" : "not connected",
                  static_cast<unsigned long>(packetsSent - before), WiFi.RSSI());
    Serial.printf("REC %lu\n", static_cast<unsigned long>(recorded));
    while (buttonHeld()) {
      delay(10);  // a hold past 6 s shouldn't start another recording
    }
    delay(50);
  }

  static String line;
  while (Serial.available() > 0) {
    const char c = static_cast<char>(Serial.read());
    if (c != '\n' && c != '\r') {
      line += c;
      continue;
    }
    if (line == "d") {
      dump();
    } else if (line == "s") {
      idleStats(false);
    } else if (line == "S") {
      idleStats(true);
    } else if (line == "t") {
      sendWhileRecording = !sendWhileRecording;
      Serial.printf("sending while recording: %s\n", sendWhileRecording ? "on" : "off");
    } else if (line.startsWith("wifi ")) {
      saveNetwork(line);
    }
    line = "";
  }

  static wl_status_t lastStatus = WL_IDLE_STATUS;
  if (WiFi.status() != lastStatus) {
    lastStatus = WiFi.status();
    if (lastStatus == WL_CONNECTED) {
      Serial.printf("wifi: connected, %s, rssi %d\n", WiFi.localIP().toString().c_str(), WiFi.RSSI());
    }
  }

  // A heartbeat on the blue LED, so it's clear the board is running.
  const uint32_t now = millis();
  digitalWrite(kLedBlue, (now % 2000) < 30 ? LOW : HIGH);
  if (now - lastStatsMs > 10000 && recorded == 0) {
    lastStatsMs = now;
    idleStats(false);
  }
  delay(5);
}
