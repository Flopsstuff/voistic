#include "board.h"

#include <driver/rtc_io.h>

#ifdef VOISTIC_BOARD_C6

#include <esp_adc/adc_cali.h>
#include <esp_adc/adc_cali_scheme.h>
#include <esp_adc/adc_oneshot.h>
#include <esp_pm.h>

namespace {

constexpr gpio_num_t kButtonGpio = GPIO_NUM_2;
// A load switch puts the battery on a 20k/10k divider into ADC1 channel 1 only
// while GPIO0 is high, so the divider doesn't drain it.
constexpr gpio_num_t kBatterySwitchGpio = GPIO_NUM_0;
constexpr adc_channel_t kBatteryChannel = ADC_CHANNEL_1;  // GPIO1
constexpr int kDividerRatio = 3;
constexpr int kBatterySamples = 16;
constexpr int kNoBatteryMv = 2500;  // below any LiPo in use, so nothing is there
constexpr gpio_num_t kMicPowerGpio = GPIO_NUM_7;
constexpr gpio_num_t kRgbPowerGpio = GPIO_NUM_18;

int batteryMv = 0;

// Oneshot, and released at once, since the microphone needs ADC1 in continuous
// mode right after.
int measureBattery() {
  adc_oneshot_unit_handle_t unit = nullptr;
  adc_oneshot_unit_init_cfg_t unitConfig = {};
  unitConfig.unit_id = ADC_UNIT_1;
  if (adc_oneshot_new_unit(&unitConfig, &unit) != ESP_OK) {
    return 0;
  }
  adc_oneshot_chan_cfg_t channelConfig = {};
  channelConfig.atten = ADC_ATTEN_DB_12;
  channelConfig.bitwidth = ADC_BITWIDTH_12;
  adc_oneshot_config_channel(unit, kBatteryChannel, &channelConfig);

  adc_cali_handle_t cali = nullptr;
  adc_cali_curve_fitting_config_t caliConfig = {};
  caliConfig.unit_id = ADC_UNIT_1;
  caliConfig.chan = kBatteryChannel;
  caliConfig.atten = ADC_ATTEN_DB_12;
  caliConfig.bitwidth = ADC_BITWIDTH_12;
  adc_cali_create_scheme_curve_fitting(&caliConfig, &cali);

  pinMode(kBatterySwitchGpio, OUTPUT);
  digitalWrite(kBatterySwitchGpio, HIGH);
  delay(2);  // the divider and its 10 nF settle in well under a millisecond
  int total = 0;
  int count = 0;
  for (int i = 0; i < kBatterySamples; ++i) {
    int raw = 0;
    int mv = 0;
    if (adc_oneshot_read(unit, kBatteryChannel, &raw) == ESP_OK && cali != nullptr &&
        adc_cali_raw_to_voltage(cali, raw, &mv) == ESP_OK) {
      total += mv;
      ++count;
    }
  }
  digitalWrite(kBatterySwitchGpio, LOW);

  if (cali != nullptr) {
    adc_cali_delete_scheme_curve_fitting(cali);
  }
  adc_oneshot_del_unit(unit);
  const int mv = count > 0 ? total / count * kDividerRatio : 0;
  return mv < kNoBatteryMv ? 0 : mv;
}

// Holds an LP pin low through deep sleep, so what it powers stays off.
void holdLow(gpio_num_t pin) {
  rtc_gpio_init(pin);
  rtc_gpio_set_direction(pin, RTC_GPIO_MODE_DISABLED);
  rtc_gpio_pullup_dis(pin);
  rtc_gpio_pulldown_en(pin);
}

}  // namespace

esp_pm_lock_handle_t noSleepLock = nullptr;
esp_pm_lock_handle_t fullSpeedLock = nullptr;

void boardBegin() {
  // Released from the sleep configuration, so they work as plain pins again.
  rtc_gpio_deinit(kMicPowerGpio);
  rtc_gpio_deinit(kBatterySwitchGpio);
  batteryMv = measureBattery();

  // Light sleep is allowed in general and held off by these locks, which only
  // the wait between presses lets go of.
  esp_pm_config_t pm = {};
  pm.max_freq_mhz = 160;
  pm.min_freq_mhz = 40;
  pm.light_sleep_enable = true;
  if (esp_pm_configure(&pm) != ESP_OK) {
    Serial.println("pm: can't configure, no light sleep");
  }
  esp_pm_lock_create(ESP_PM_NO_LIGHT_SLEEP, 0, "awake", &noSleepLock);
  esp_pm_lock_create(ESP_PM_CPU_FREQ_MAX, 0, "fast", &fullSpeedLock);
  boardAllowLightSleep(false);
}

void boardMeasureBattery() { batteryMv = measureBattery(); }

void boardAllowLightSleep(bool allow) {
  static bool held = false;
  if (noSleepLock == nullptr || fullSpeedLock == nullptr || held == !allow) {
    return;
  }
  held = !allow;
  if (allow) {
    esp_pm_lock_release(fullSpeedLock);
    esp_pm_lock_release(noSleepLock);
  } else {
    esp_pm_lock_acquire(noSleepLock);
    esp_pm_lock_acquire(fullSpeedLock);
  }
}

bool boardWokenByButton() { return esp_sleep_get_wakeup_cause() == ESP_SLEEP_WAKEUP_EXT1; }

int boardBatteryMv() { return batteryMv; }

void boardSleep() {
  // GPIO18 isn't an LP pin, so it can't be held through deep sleep. The RGB
  // LED's load switch is expected to pull its enable low by itself, which the
  // sleep current should confirm.
  digitalWrite(kRgbPowerGpio, LOW);
  holdLow(kMicPowerGpio);
  holdLow(kBatterySwitchGpio);

  // Power management leaves a timer armed for light sleep, which would wake
  // the board from deep sleep at once, so only the button is kept.
  esp_sleep_disable_wakeup_source(ESP_SLEEP_WAKEUP_ALL);
  rtc_gpio_pullup_en(kButtonGpio);
  rtc_gpio_pulldown_dis(kButtonGpio);
  esp_sleep_enable_ext1_wakeup_io(1ULL << kButtonGpio, ESP_EXT1_WAKEUP_ANY_LOW);
  esp_deep_sleep_start();
}

#else  // XIAO ESP32S3

namespace {

constexpr gpio_num_t kButtonGpio = GPIO_NUM_3;
constexpr gpio_num_t kPixelGpio = GPIO_NUM_4;
constexpr uint8_t kLedPin = LED_BUILTIN;  // GPIO21, active low

}  // namespace

void boardBegin() {
  pinMode(kLedPin, OUTPUT);
  digitalWrite(kLedPin, HIGH);
}

bool boardWokenByButton() { return esp_sleep_get_wakeup_cause() == ESP_SLEEP_WAKEUP_EXT0; }

int boardBatteryMv() { return 0; }

void boardMeasureBattery() {}

void boardAllowLightSleep(bool) {}

void boardSleep() {
  digitalWrite(kLedPin, HIGH);

  rtc_gpio_pullup_en(kButtonGpio);
  rtc_gpio_pulldown_dis(kButtonGpio);

  // Keeps the data line from floating, which could light the pixel during sleep.
  rtc_gpio_init(kPixelGpio);
  rtc_gpio_pullup_dis(kPixelGpio);
  rtc_gpio_pulldown_en(kPixelGpio);

  esp_sleep_enable_ext0_wakeup(kButtonGpio, 0);
  esp_deep_sleep_start();
}

#endif
