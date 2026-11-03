// Wi-Fi button on a XIAO ESP32S3 Plus with an IoT Button with NeoPixel BFF, or
// on a Seeed IoT Button V2, see board.h.
//
// The board sleeps until pressed. It recognises the gesture, sends it to the
// paired server, shows the outcome and goes back to sleep. What a gesture does
// is configured on the server. See gesture.h for the gestures and leds.cpp for
// what each colour means.
//
// A board that stays connected (board.h) runs each press in its own task
// instead, which finish() ends, and loop() waits for the next press in light
// sleep with Wi-Fi up, then deep-sleeps after the time the server asked for.

#include <Arduino.h>
#include <WiFi.h>
#include <esp_ota_ops.h>
#ifdef VOISTIC_BOARD_C6
#include <hal/gpio_ll.h>
#endif

#include "board.h"
#include "client.h"
#include "gesture.h"
#include "leds.h"
#include "link.h"
#include "mic.h"
#include "portal.h"

// Arduino would otherwise accept a new firmware as soon as it boots. It's kept on
// probation instead, until confirmFirmware has reached the server.
extern "C" bool verifyRollbackLater() { return true; }

namespace {

constexpr gpio_num_t kButtonGpio = static_cast<gpio_num_t>(kButtonPin);

constexpr uint32_t kAwakeLimitMs = 15000;
constexpr uint32_t kIdleAfterResetMs = 10000;  // room for a USB reflash or a hold
constexpr uint32_t kJoinTimeoutMs = 15000;
constexpr uint32_t kPairTimeoutMs = 120000;
constexpr uint32_t kPairCancelHoldMs = 2000;
constexpr uint32_t kPortalTimeoutMs = 300000;
constexpr uint32_t kMaxRecordingMs = 180000;
constexpr size_t kMaxEdges = 32;

struct Edge {
  uint32_t ms;
  bool down;
};

// Filled by the interrupt, because WiFi.mode() blocks the loop for tens of
// milliseconds, which is long enough to miss part of a fast double press.
Edge edges[kMaxEdges];
volatile size_t edgeHead = 0;
size_t edgeTail = 0;

GestureDetector* detector = nullptr;
bool wokenByPress = false;
uint32_t cycleStartMs = 0;  // when this press began, the boot or a wake from waiting
bool cycleOnTask = false;   // each press runs in its own task, see loop()
TaskHandle_t supervisor = nullptr;
bool linkStarted = false;
bool networkSaved = false;
bool warmedUp = false;
const char* pendingGesture = nullptr;
Led pendingLook = Led::Off;

void IRAM_ATTR onButtonEdge() {
  const size_t head = edgeHead;
  const size_t next = (head + 1) % kMaxEdges;
  if (next != edgeTail) {
    edges[head] = {static_cast<uint32_t>(esp_timer_get_time() / 1000),
                   gpio_get_level(kButtonGpio) == 0};
    edgeHead = next;
  }
}

bool buttonDown() { return digitalRead(kButtonPin) == LOW; }

void readButton() {
  while (edgeTail != edgeHead) {
    detector->update(edges[edgeTail].ms, edges[edgeTail].down);
    edgeTail = (edgeTail + 1) % kMaxEdges;
  }
  // Also corrects any level the interrupt read wrongly during a bounce.
  detector->update(millis(), buttonDown());
}

[[noreturn]] void sleepNow() {
  // The boot's own line goes out before a USB port is open, so it's repeated here.
  Serial.printf("sleep, after a boot with reset %d, wake cause %d\n", esp_reset_reason(),
                esp_sleep_get_wakeup_cause());
  detachInterrupt(digitalPinToInterrupt(kButtonPin));
  ledsStop();
  boardSleep();
}

// Whether this press can end by waiting connected for the next one.
bool canStayConnected() {
  return cycleOnTask && clientIsPaired() && WiFi.status() == WL_CONNECTED;
}

// Shows the final state, then sleeps once the button is up, since a held button
// would wake the board again at once. A low battery adds its warning. Where the
// board stays connected, it ends this press's task instead.
[[noreturn]] void finish(Led outcome) {
  ledsSet(outcome);
  const bool stay = canStayConnected();
  if (!stay) {
    WiFi.disconnect(/*wifioff=*/true);  // the fade doesn't need the radio
  }
  ledsWait();
  const int battery = boardBatteryMv();
  if (battery > 0 && battery < kBatteryLowMv) {
    ledsSet(Led::BatteryLow);
    ledsWait();
  }
  ledsSet(Led::Off);
  while (buttonDown()) {
    delay(10);
  }
  if (stay) {
    xTaskNotifyGive(supervisor);
    vTaskDelete(nullptr);
  }
  sleepNow();
}

const char* gestureName(Gesture gesture) {
  switch (gesture) {
    case Gesture::Single:
      return "single";
    case Gesture::Double:
      return "double";
    case Gesture::Triple:
      return "triple";
    case Gesture::Long:
      return "long";
    default:
      return nullptr;
  }
}

Led pressLook() {
  switch (detector->pressCount()) {
    case 1:
      return Led::Press1;
    case 2:
      return Led::Press2;
    case 3:
      return Led::Press3;
    default:
      return Led::Press4;
  }
}

void startLink() {
  if (!linkStarted) {
    linkStarted = true;
    networkSaved = linkStart();
    if (!networkSaved) {
      Serial.println("wifi: no saved network");
    }
  }
}

bool joinSavedNetwork() {
  const uint32_t start = millis();
  while (networkSaved && millis() - start < kJoinTimeoutMs) {
    if (linkPoll(millis())) {
      return true;
    }
    delay(10);
  }
  return false;
}

// A 2 s press gives up on pairing. It's read from the edges, because a network
// query blocks for seconds, so a press in the middle of one still counts.
bool pairingCancelled() {
  static bool down = false;
  static uint32_t downAtMs = 0;
  while (edgeTail != edgeHead) {
    const Edge edge = edges[edgeTail];
    edgeTail = (edgeTail + 1) % kMaxEdges;
    if (edge.down && !down) {
      down = true;
      downAtMs = edge.ms;
    } else if (!edge.down && down) {
      down = false;
      if (edge.ms - downAtMs >= kPairCancelHoldMs) {
        return true;
      }
    }
  }
  return down && millis() - downAtMs >= kPairCancelHoldMs;
}

[[noreturn]] void runPairing() {
  Serial.printf("pairing as %s\n", clientDeviceId().c_str());
  ledsSet(Led::Pairing);
  startLink();
  if (!joinSavedNetwork()) {
    ledsSet(Led::Portal);
    if (!portalRun(kPortalTimeoutMs)) {
      finish(Led::Failed);
    }
    ledsSet(Led::Pairing);
  }
  clientRelease();  // the server won't hand a new key to a button that has one
  edgeTail = edgeHead;  // the press that got here doesn't count
  finish(clientPair(kPairTimeoutMs, pairingCancelled) ? Led::Paired : Led::Failed);
}

[[noreturn]] void runUpdate() {
#ifdef VOISTIC_MIC
  micStop();
#endif
  ledsSet(pressLook(), /*half=*/true);
  startLink();
  UpdateOffer offer;
  const UpdateCheck check = joinSavedNetwork() ? clientCheckUpdate(offer) : UpdateCheck::Failed;
  if (check != UpdateCheck::Offered) {
    finish(check == UpdateCheck::Current ? Led::Accepted : Led::Failed);
  }
  ledsSet(Led::Updating);
  if (!clientDownloadUpdate(offer)) {
    finish(Led::Failed);
  }
  esp_restart();
}

bool firmwareOnProbation() {
  esp_ota_img_states_t state;
  return esp_ota_get_state_partition(esp_ota_get_running_partition(), &state) == ESP_OK &&
         state == ESP_OTA_IMG_PENDING_VERIFY;
}

// The first boot of a new firmware. If it can't reach the server, the bootloader
// goes back to the one before. So does a crash, which never gets this far.
[[noreturn]] void confirmFirmware() {
  ledsSet(Led::Updating);
  startLink();
  UpdateOffer offer;
  if (joinSavedNetwork() && clientCheckUpdate(offer) != UpdateCheck::Failed) {
    esp_ota_mark_app_valid_cancel_rollback();
    Serial.println("firmware: confirmed");
    finish(Led::Accepted);
  }
  Serial.println("firmware: can't reach the server, going back");
  esp_ota_mark_app_invalid_rollback_and_reboot();
  esp_restart();
}

[[noreturn]] void sendPending(const char* gesture) {
  ledsSet(pendingLook, /*half=*/true);
  Serial.printf("gesture: %s at %lu ms\n", gesture, millis());
  const SendResult result = clientSendPress(gesture);
  Serial.printf("press: answered at %lu ms\n", millis());
  finish(result == SendResult::Accepted ? Led::Accepted : Led::Failed);
}

#ifdef VOISTIC_MIC
// Streams the voice until the button is released. Everything since the wake
// goes first, so what was said before the light turned on isn't lost.
[[noreturn]] void recordVoice() {
  ledsSet(Led::HoldWaiting);
  if (!clientAudioBegin(kMicRate)) {
    finish(Led::Failed);
  }
  ledsSet(Led::HoldLong);

  static uint8_t chunk[1024];
  const uint32_t start = millis();
  bool sent = true;
  while (sent && detector->isDown() && millis() - start < kMaxRecordingMs) {
    readButton();
    const size_t length = micRead(chunk, sizeof(chunk));
    if (length > 0) {
      sent = clientAudioSend(chunk, length);
    } else {
      delay(2);
    }
  }

  micStop();
  ledsSet(Led::HoldWaiting, /*half=*/true);
  for (size_t length = micRead(chunk, sizeof(chunk)); sent && length > 0;
       length = micRead(chunk, sizeof(chunk))) {
    sent = clientAudioSend(chunk, length);
  }
  finish(sent && clientAudioEnd() == SendResult::Accepted ? Led::Accepted : Led::Failed);
}
#endif

// `wifi ssid|password` sets the network over USB, without the setup page, and
// `brightness 0.2` the pixel, from 0 to 1. Both stay on the board.
void handleCommand(const String& line) {
  if (line.startsWith("brightness ")) {
    const float brightness = line.substring(11).toFloat();
    if (isDigit(line[11]) && brightness <= 1.0f) {
      clientSaveBrightness(brightness);
      Serial.printf("brightness: saved %.2f\n", brightness);
      return;
    }
  }
  const int separator = line.indexOf('|');
  if (!line.startsWith("wifi ") || separator < 0) {
    Serial.println("usage: wifi ssid|password, brightness 0.2");
    return;
  }
  const String ssid = line.substring(5, separator);
  linkSave(ssid.c_str(), line.substring(separator + 1).c_str());
  linkStarted = true;
  networkSaved = true;
  Serial.printf("wifi: saved %s\n", ssid.c_str());
}

void pollSerial() {
  static String line;

  while (Serial.available() > 0) {
    const char c = static_cast<char>(Serial.read());
    if (c != '\r' && c != '\n') {
      line += c;
    } else if (line.length() > 0) {
      handleCommand(line);
      line = "";
    }
  }
}

void showProgress(uint32_t now) {
  switch (detector->hold(now)) {
    case Hold::LongWaiting:
      ledsSet(Led::HoldWaiting);
      return;
    case Hold::Long:
      ledsSet(Led::HoldLong);
      return;
    case Hold::Armed:
      ledsSet(Led::HoldArmed);
      return;
    case Hold::Pair:
      ledsSet(Led::HoldPair);
      return;
    case Hold::Reset:
      ledsSet(Led::HoldReset);
      return;
    case Hold::None:
      break;
  }
  if (detector->pressCount() == 0) {
    ledsSet(Led::Off);
  } else {
    ledsSet(pressLook(), /*half=*/!detector->isDown());
  }
}

}  // namespace

namespace {

// The rest of the first press, after the boot.
void beginFirstCycle() {
  cycleStartMs = millis();
  if (firmwareOnProbation()) {
    confirmFirmware();
  }
  if (wokenByPress) {
    startLink();  // already on its way by the time the gesture is known
#ifdef VOISTIC_MIC
    micStart();
#endif
  }
}

// A press that woke the board from waiting connected.
void beginNextCycle() {
  cycleStartMs = millis();
  wokenByPress = true;
  pendingGesture = nullptr;
  pendingLook = Led::Off;
  warmedUp = false;  // the server's address may have dropped out of the ARP cache
  edgeTail = edgeHead;
  delete detector;
  detector = new GestureDetector(buttonDown(), /*wokenByPress=*/true, cycleStartMs);
#ifdef VOISTIC_MIC
  micStart();
#endif
}

void runCycle() {
  const uint32_t now = millis();
  readButton();
  pollSerial();

  if (detector->pressCount() > 0) {
    startLink();  // the first press after a reset, a wake press is already on it
#ifdef VOISTIC_MIC
    micStart();
#endif
  }
  const bool connected = linkStarted && linkPoll(now);
  if (connected) {
    detector->linkUp();
    if (!warmedUp) {
      warmedUp = true;
      clientWarmUp();
    }
  }

  const Gesture gesture = detector->take();
  if (gesture == Gesture::Ignored) {
    finish(Led::Off);
  } else if (gesture == Gesture::Pair) {
    runPairing();
  } else if (gesture == Gesture::Reset) {
    startLink();
    joinSavedNetwork();  // to tell the server, while the network is still known
    clientForget();
    linkForget();
    linkStarted = false;
    runPairing();
  } else if (gesture != Gesture::None && !clientIsPaired()) {
    runPairing();  // a press on a new or reset button starts its setup
  } else if (gesture == Gesture::Update) {
    runUpdate();
  } else if (gesture != Gesture::None) {
    pendingGesture = gestureName(gesture);
    pendingLook = gesture == Gesture::Long ? Led::HoldWaiting : pressLook();
  }

  if (pendingGesture != nullptr && connected) {
    sendPending(pendingGesture);
  }
#ifdef VOISTIC_MIC
  if (detector->hold(now) == Hold::Long && clientIsPaired()) {
    recordVoice();
  }
#endif

  if (!detector->isDown()) {
    const uint32_t elapsed = now - cycleStartMs;
    if (elapsed >= kAwakeLimitMs) {
      finish(pendingGesture != nullptr ? Led::Failed : Led::Off);
    }
    if (!wokenByPress && pendingGesture == nullptr && elapsed >= kIdleAfterResetMs) {
      finish(Led::Off);
    }
  }

  showProgress(now);
  delay(1);
}

void cycleTask(void* first) {
  if (first != nullptr) {
    beginFirstCycle();
  } else {
    beginNextCycle();
  }
  for (;;) {
    runCycle();
  }
}

void startCycleTask(bool first) {
  // As much stack as Arduino gives loop().
  xTaskCreate(cycleTask, "press", 8192, first ? &supervisor : nullptr, 1, nullptr);
}

#ifdef VOISTIC_BOARD_C6
TaskHandle_t waiter = nullptr;

// Level-triggered, so it wakes the chip from light sleep, and switched off at
// once, or it would fire for as long as the button is down.
void IRAM_ATTR onWaitingPress() {
  gpio_ll_intr_disable(&GPIO, kButtonGpio);
  BaseType_t woken = pdFALSE;
  vTaskNotifyGiveFromISR(waiter, &woken);
  portYIELD_FROM_ISR(woken);
}

// Waits for a press in light sleep, with Wi-Fi in power save but connected.
// Returns false when the time runs out or the network goes.
bool waitConnected(uint32_t limitMs) {
#ifdef VOISTIC_MIC
  micStop();
#endif
  ledsStop();
  WiFi.setSleep(true);
  waiter = xTaskGetCurrentTaskHandle();
  detachInterrupt(digitalPinToInterrupt(kButtonPin));
  ulTaskNotifyTake(pdTRUE, 0);  // nothing left over from before
  attachInterrupt(digitalPinToInterrupt(kButtonPin), onWaitingPress, ONLOW_WE);
  esp_sleep_enable_gpio_wakeup();
  Serial.printf("wait: connected, up to %lu s\n", static_cast<unsigned long>(limitMs / 1000));
  boardAllowLightSleep(true);

  const uint32_t start = millis();
  bool pressed = false;
  while (millis() - start < limitMs) {
    if (buttonDown()) {
      pressed = true;
      break;
    }
    if (WiFi.status() != WL_CONNECTED) {
      break;
    }
    ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(5000));
  }

  boardAllowLightSleep(false);
  detachInterrupt(digitalPinToInterrupt(kButtonPin));
  attachInterrupt(digitalPinToInterrupt(kButtonPin), onButtonEdge, CHANGE);
  WiFi.setSleep(false);
  ledsBegin();
  Serial.printf("wait: %s after %lu s\n",
                pressed ? "pressed" : (WiFi.status() == WL_CONNECTED ? "time is up" : "Wi-Fi lost"),
                static_cast<unsigned long>((millis() - start) / 1000));
  if (pressed) {
    boardMeasureBattery();
  }
  return pressed;
}
#endif

}  // namespace

void setup() {
  const uint32_t startupMs = millis();
  pinMode(kButtonPin, INPUT_PULLUP);
  const bool downAtStartup = buttonDown();
  attachInterrupt(digitalPinToInterrupt(kButtonPin), onButtonEdge, CHANGE);
  wokenByPress = boardWokenByButton();

  Serial.begin(115200);
  // Short, so a closed port only costs a few ms per write. Never 0: HWCDC::write
  // counts retries down from this value, so 0 wraps around and hangs the loop
  // once the TX buffer fills.
  Serial.setTxTimeoutMs(10);

  boardBegin();
  ledsBegin();
  clientRestoreBrightness();

  Serial.printf("boot: %s %s, %s, fw %s, wake %d, paired %d, battery %d mV\n", __DATE__, __TIME__,
                VOISTIC_BOARD, FW_VERSION, wokenByPress, clientIsPaired(), boardBatteryMv());

  // The level read before anything slow, so a short press isn't missed.
  detector = new GestureDetector(downAtStartup, wokenByPress, startupMs);
  supervisor = xTaskGetCurrentTaskHandle();
  cycleOnTask = kBoardStaysConnected;
  if (cycleOnTask) {
    startCycleTask(/*first=*/true);
  } else {
    beginFirstCycle();
  }
}

void loop() {
  if (!cycleOnTask) {
    runCycle();
    return;
  }
#ifdef VOISTIC_BOARD_C6
  ulTaskNotifyTake(pdTRUE, portMAX_DELAY);  // the press's task has ended
  const int seconds = clientAwakeSeconds();
  const uint32_t limitMs = seconds >= 0 ? seconds * 1000UL : kDefaultStayConnectedMs;
  if (limitMs == 0 || !waitConnected(limitMs)) {
    WiFi.disconnect(/*wifioff=*/true);
    sleepNow();
  }
  startCycleTask(/*first=*/false);
#endif
}
