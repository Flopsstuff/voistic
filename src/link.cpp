#include "link.h"

#include <Arduino.h>
#include <WiFi.h>
#include <esp_wifi.h>

namespace {

// Long enough for a cached join, after which a moved or restarted access point
// falls back to a normal scan.
constexpr uint32_t kCachedJoinMs = 3000;

RTC_DATA_ATTR int32_t cachedChannel = 0;
RTC_DATA_ATTR uint8_t cachedBssid[6] = {};

// An access point that still counts the board as connected, after a crash or a
// reflash, refuses the first association, and the driver then gives up. A
// second try a moment later gets in.
constexpr uint32_t kRetryAfterMs = 1000;
constexpr int kMaxRetries = 3;

wifi_config_t stored = {};
uint32_t startedAtMs = 0;
bool usingCache = false;
bool saved = false;
volatile bool dropped = false;
uint32_t droppedAtMs = 0;
int retries = 0;

const char* ssid() { return reinterpret_cast<const char*>(stored.sta.ssid); }
const char* password() { return reinterpret_cast<const char*>(stored.sta.password); }

void onDisconnected(WiFiEvent_t, WiFiEventInfo_t) {
  droppedAtMs = millis();
  dropped = true;
}

}  // namespace

bool linkStart() {
  static bool listening = false;
  if (!listening) {
    listening = true;
    WiFi.onEvent(onDisconnected, ARDUINO_EVENT_WIFI_STA_DISCONNECTED);
  }
  // Modem sleep saves nothing on a board that's awake for seconds, and it delays
  // the join: medians of 300 ms without it against 550 ms with it.
  WiFi.setSleep(false);
  WiFi.mode(WIFI_STA);
  // WiFi.SSID() only knows a connected network, so ask the driver.
  esp_wifi_get_config(WIFI_IF_STA, &stored);
  if (ssid()[0] == '\0') {
    return false;
  }

  startedAtMs = millis();
  usingCache = cachedChannel != 0;
  if (usingCache) {
    WiFi.begin(ssid(), password(), cachedChannel, cachedBssid, true);
  } else {
    WiFi.begin(ssid(), password());
  }
  return true;
}

void linkSave(const char* newSsid, const char* newPassword) {
  WiFi.mode(WIFI_STA);
  WiFi.begin(newSsid, newPassword);  // the driver stores them
  startedAtMs = millis();
  usingCache = false;
  cachedChannel = 0;
  saved = false;
}

void linkForget() {
  WiFi.mode(WIFI_STA);
  WiFi.disconnect(/*wifioff=*/false, /*eraseap=*/true);
  cachedChannel = 0;
  saved = false;
}

bool linkPoll(uint32_t nowMs) {
  if (WiFi.status() == WL_CONNECTED) {
    if (!saved) {
      saved = true;
      cachedChannel = WiFi.channel();
      memcpy(cachedBssid, WiFi.BSSID(), sizeof(cachedBssid));
      Serial.printf("wifi: connected in %lu ms, at %lu ms, rssi %d\n",
                    static_cast<unsigned long>(nowMs - startedAtMs), static_cast<unsigned long>(nowMs), WiFi.RSSI());
    }
    return true;
  }

  if (dropped && !usingCache && retries < kMaxRetries && nowMs - droppedAtMs >= kRetryAfterMs) {
    dropped = false;
    ++retries;
    Serial.printf("wifi: dropped, trying again (%d)\n", retries);
    WiFi.begin(ssid(), password());
    return false;
  }

  if (usingCache && nowMs - startedAtMs >= kCachedJoinMs) {
    Serial.println("wifi: cached join failed, scanning");
    usingCache = false;
    cachedChannel = 0;
    WiFi.disconnect();
    WiFi.begin(ssid(), password());
  }
  return false;
}
