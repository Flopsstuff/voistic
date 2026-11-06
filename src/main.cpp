// Wi-Fi button on a XIAO ESP32S3 Plus with an IoT Button with NeoPixel BFF, or
// on a Seeed IoT Button V2, see board.h.
//
// The board sleeps until pressed. It recognises the gesture, sends it to the
// paired server, shows the outcome and goes back to sleep. What a gesture does
// is configured on the server. See gesture.h for the gestures and leds.cpp for
// what each colour means.
//
// Each press runs in its own task, which finish() ends. loop() then keeps the
// board fully awake for the server's ready_s, with Wi-Fi and the microphone on,
// so a quick next press starts at once. Presses made meanwhile, even during the
// last one's answer or its green fade, are kept with their times and replayed.
// After that a board that stays connected (board.h) waits for the next press
// in light sleep with Wi-Fi up, and deep-sleeps after the server's awake_s.

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
constexpr uint32_t kDefaultReadyMs = 30000;  // without a word from the server
constexpr uint32_t kIdleAfterResetMs = 10000;  // room for a USB reflash or a hold
constexpr uint32_t kJoinTimeoutMs = 15000;
constexpr uint32_t kPairTimeoutMs = 120000;
constexpr uint32_t kPairCancelHoldMs = 2000;
constexpr uint32_t kPortalTimeoutMs = 300000;
constexpr uint32_t kMaxRecordingMs = 180000;
constexpr uint32_t kUsbIdleMs = 30000;  // a USB session ends this long after its last command
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
TaskHandle_t supervisor = nullptr;
// How the next press's task starts: from waiting ready, with its edges in the
// buffer, or from a wake out of light sleep at wakeMs, with the button down.
bool nextFromReady = false;
uint32_t wakeMs = 0;
bool linkStarted = false;
bool networkSaved = false;
bool warmedUp = false;
const char* pendingGesture = nullptr;
Led pendingLook = Led::Off;
bool usbSession = false;  // a host is working on the board over USB
uint32_t usbLastMs = 0;

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
bool canStayConnected() { return clientIsPaired() && WiFi.status() == WL_CONNECTED; }

// Shows the final state and ends this press's task, leaving the wait for the
// next press to loop(). The outcome keeps fading meanwhile, and a press cuts it
// short. Without the network there's nothing to wait for, so the board sleeps
// once the button is up, since a held button would wake it again at once. A low
// battery adds its warning, which waits for the fade.
[[noreturn]] void finish(Led outcome) {
  ledsSet(outcome);
  const bool stay = canStayConnected();
  if (!stay) {
    WiFi.disconnect(/*wifioff=*/true);  // the fade doesn't need the radio
  }
  const int battery = boardBatteryMv();
  const bool low = battery > 0 && battery < kBatteryLowMv;
  if (!stay || low) {
    ledsWait();
    if (low) {
      ledsSet(Led::BatteryLow);
      ledsWait();
    }
    ledsSet(Led::Off);
  }
  if (stay) {
    if (buttonDown()) {
      // Still down from this press, such as a recording at its limit, which
      // isn't the start of the next one.
      while (buttonDown()) {
        delay(10);
      }
      edgeTail = edgeHead;
    }
    xTaskNotifyGive(supervisor);
    vTaskDelete(nullptr);
  }
  while (buttonDown()) {
    delay(10);
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

// Commands over USB, one a line, each answered by one line that starts with its
// name, which is what `voistic usb` speaks. The first `hello` holds the board
// awake and leaves the button alone until `bye`, or until kUsbIdleMs without a
// command, so the host can work on it at its own pace.
//
//   hello                     starts the session, answers with the status
//   status                    {"device":..., "paired":..., "connected":...}
//   wifi ssid|password        saves a network and joins it
//   brightness 0.2            the pixel, from 0 to 1
//   pair key host port        a key from the server, in place of pairing over Wi-Fi
//   check                     asks the paired server, which proves key and network
//   update                    takes the firmware the server holds for it, and restarts
//   reset                     forgets the network and the server
//   bye                       ends the session; the board carries on as after a press

String jsonEscape(const String& text) {
  String escaped;
  for (const char c : text) {
    if (c == '"' || c == '\\') {
      escaped += '\\';
    }
    if (static_cast<uint8_t>(c) >= 0x20) {
      escaped += c;
    }
  }
  return escaped;
}

String statusJson() {
  const bool connected = WiFi.status() == WL_CONNECTED;
  String json = "{\"device\":\"" + clientDeviceId() + "\",\"board\":\"" VOISTIC_BOARD
                "\",\"fw\":\"" FW_VERSION "\",\"paired\":" +
                String(clientIsPaired() ? "true" : "false") + ",\"server\":\"" +
                jsonEscape(clientServer()) + "\",\"ssid\":\"" + jsonEscape(linkSavedSsid()) +
                "\",\"connected\":" + String(connected ? "true" : "false");
  if (connected) {
    json += ",\"ip\":\"" + WiFi.localIP().toString() + "\",\"rssi\":" + String(WiFi.RSSI());
  }
#ifdef VOISTIC_MIC
  json += ",\"mic\":true";
#endif
  return json + ",\"battery_mv\":" + String(boardBatteryMv()) +
         ",\"brightness\":" + String(ledsBrightness(), 2) + "}";
}

void handleCommand(const String& line) {
  if (!usbSession && line != "hello" && !line.startsWith("wifi ") &&
      !line.startsWith("brightness ")) {
    Serial.println("error: say hello first");
    return;
  }
  usbLastMs = millis();

  if (line == "hello") {
    usbSession = true;
    Serial.printf("usb: session, the button is ignored until bye\n");
    startLink();  // so the status can tell whether the network works
    Serial.println("hello: " + statusJson());
  } else if (line == "status") {
    Serial.println("status: " + statusJson());
  } else if (line.startsWith("brightness ")) {
    const float brightness = line.substring(11).toFloat();
    if (!isDigit(line[11]) || brightness > 1.0f) {
      Serial.println("error: brightness goes from 0 to 1");
      return;
    }
    clientSaveBrightness(brightness);
    Serial.printf("brightness: saved %.2f\n", brightness);
  } else if (line.startsWith("wifi ")) {
    const int separator = line.indexOf('|');
    if (separator < 0) {
      Serial.println("error: wifi ssid|password");
      return;
    }
    const String ssid = line.substring(5, separator);
    linkSave(ssid.c_str(), line.substring(separator + 1).c_str());
    linkStarted = true;
    networkSaved = true;
    Serial.printf("wifi: saved %s\n", ssid.c_str());
  } else if (line.startsWith("pair ")) {
    const int hostAt = line.indexOf(' ', 5);
    const int portAt = hostAt < 0 ? -1 : line.indexOf(' ', hostAt + 1);
    if (portAt < 0 || !clientAdopt(line.substring(5, hostAt), line.substring(hostAt + 1, portAt),
                                   line.substring(portAt + 1).toInt())) {
      Serial.println("error: pair key host port");
      return;
    }
    Serial.printf("pair: saved for %s\n", clientServer().c_str());
  } else if (line == "check") {
    startLink();
    UpdateOffer offer;
    const UpdateCheck check = !clientIsPaired()      ? UpdateCheck::Failed
                              : !joinSavedNetwork() ? UpdateCheck::Failed
                                                    : clientCheckUpdate(offer);
    Serial.printf("check: %s\n", check == UpdateCheck::Current   ? "ok"
                                 : check == UpdateCheck::Offered ? "update waiting"
                                 : !clientIsPaired()             ? "not paired"
                                 : WiFi.status() != WL_CONNECTED ? "no network"
                                                                 : "no answer from the server");
  } else if (line == "update") {
    startLink();
    UpdateOffer offer;
    const UpdateCheck check = !clientIsPaired()      ? UpdateCheck::Failed
                              : !joinSavedNetwork() ? UpdateCheck::Failed
                                                    : clientCheckUpdate(offer);
    if (check == UpdateCheck::Current) {
      Serial.println("update: current");
      return;
    }
    if (check == UpdateCheck::Failed) {
      Serial.println("error: can't reach the server");
      return;
    }
    ledsSet(Led::Updating);
    if (!clientDownloadUpdate(offer)) {
      ledsSet(Led::Failed);
      Serial.println("error: the download failed");
      return;
    }
    Serial.printf("update: installed %s, restarting\n", offer.version.c_str());
    Serial.flush();
    delay(100);
    esp_restart();
  } else if (line == "reset") {
    startLink();
    joinSavedNetwork();  // to tell the server, while the network is still known
    clientForget();
    linkForget();
    linkStarted = false;
    networkSaved = false;
    Serial.println("reset: done");
  } else if (line == "bye") {
    Serial.println("bye: done, carrying on as after a press");
    usbSession = false;
    finish(Led::Off);
  } else {
    Serial.println("error: unknown command " + line);
  }
}

void pollSerial() {
  static String line;

  while (Serial.available() > 0) {
    const char c = static_cast<char>(Serial.read());
    if (c != '\r' && c != '\n') {
      line += c;
    } else if (line.length() > 0) {
      handleCommand(line);
      usbLastMs = millis();  // a slow command doesn't eat into the idle time
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

// A press after the last one, while waiting ready or connected. The edges
// since are still in the buffer, and readButton() replays them with their
// times, so a quick press isn't lost.
void beginNextCycle() {
  cycleStartMs = millis();
  wokenByPress = true;
  pendingGesture = nullptr;
  pendingLook = Led::Off;
  ledsSet(Led::Off);  // cuts the last outcome short, and lets it play again
  delete detector;
  if (nextFromReady) {
    // The audio since the last press ended is kept, see waitReady().
    detector = new GestureDetector(/*down=*/false, /*wokenByPress=*/false, cycleStartMs);
  } else {
    // The press woke it with the button down. If it's up already and its
    // release missed the buffer, it still counts as a press.
    warmedUp = false;  // the server's address may have dropped out of the ARP cache
    detector = new GestureDetector(buttonDown() || edgeTail != edgeHead, /*wokenByPress=*/true, wakeMs);
  }
#ifdef VOISTIC_MIC
  micStart();  // a no-op if it's still running
#endif
}

void runCycle() {
  const uint32_t now = millis();
  if (pendingGesture == nullptr) {
    readButton();  // once a gesture is taken, later presses wait for the next task
  }
  pollSerial();

  if (usbSession) {
    // Read afresh: a command just now may be later than the cycle's start.
    if (millis() - usbLastMs >= kUsbIdleMs) {
      Serial.println("usb: no command for a while, ending the session");
      usbSession = false;
      finish(Led::Off);
    }
    if (linkStarted) {
      linkPoll(now);
    }
    delay(1);
    return;
  }

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
#ifdef VOISTIC_MIC
    // This press is over, so the next recording starts from here, even if
    // its hold begins while this one is still being answered.
    micDiscard();
#endif
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

// Whether a press came since the last task stopped reading the button. Until
// one does, the audio is dropped as it comes, so a recording starts within a
// few ms of its press, and the buffer never fills with silence.
bool pressWaiting() {
  if (edgeTail != edgeHead || buttonDown()) {
    return true;
  }
#ifdef VOISTIC_MIC
  micDiscard();
#endif
  return false;
}

// Keeps the board fully awake after a press, with Wi-Fi and the microphone on,
// so a quick next press starts at once. Then lets the outcome's fade play out.
// Returns true on a press, which may have come before this was even called.
bool waitReady(uint32_t limitMs) {
  if (limitMs > 0) {
#ifdef VOISTIC_MIC
    micStart();  // warm, in case the next press is a hold
#endif
    Serial.printf("ready: up to %lu s\n", static_cast<unsigned long>(limitMs / 1000));
  }
  const uint32_t start = millis();
  while (millis() - start < limitMs && WiFi.status() == WL_CONNECTED) {
    if (pressWaiting()) {
      return true;
    }
    delay(5);
  }
  while (!ledsDone()) {
    if (pressWaiting()) {
      return true;
    }
    delay(5);
  }
  return pressWaiting();
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
  for (int i = 0; !micIdle() && i < 100; ++i) {
    delay(2);  // the battery shares the ADC
  }
#endif
  // Measured now rather than after the wake, so the microphone can start first
  // then. The next press reports it.
  boardMeasureBattery();
  ledsStop();
  WiFi.setSleep(true);
  waiter = xTaskGetCurrentTaskHandle();
  detachInterrupt(digitalPinToInterrupt(kButtonPin));
  // A press that came, and maybe went, while this was setting up is in the
  // buffer with its times. It's replayed as one from waiting ready, from the
  // button up: the wake's own start would come after its edges.
  nextFromReady = edgeTail != edgeHead;
  ulTaskNotifyTake(pdTRUE, 0);  // nothing left over from before
  attachInterrupt(digitalPinToInterrupt(kButtonPin), onWaitingPress, ONLOW_WE);
  esp_sleep_enable_gpio_wakeup();
  Serial.printf("wait: connected, up to %lu s\n", static_cast<unsigned long>(limitMs / 1000));
  boardAllowLightSleep(true);

  const uint32_t start = millis();
  bool pressed = nextFromReady;
  while (!pressed && millis() - start < limitMs) {
    if (buttonDown()) {
      pressed = true;
      break;
    }
    if (WiFi.status() != WL_CONNECTED) {
      break;
    }
    ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(5000));
  }

  wakeMs = millis();
  boardAllowLightSleep(false);
  detachInterrupt(digitalPinToInterrupt(kButtonPin));
  attachInterrupt(digitalPinToInterrupt(kButtonPin), onButtonEdge, CHANGE);
#ifdef VOISTIC_MIC
  if (pressed) {
    micStart();  // first, since the voice is already coming
  }
#endif
  WiFi.setSleep(false);
  ledsBegin();
  Serial.printf("wait: %s after %lu s\n",
                pressed ? "pressed" : (WiFi.status() == WL_CONNECTED ? "time is up" : "Wi-Fi lost"),
                static_cast<unsigned long>((millis() - start) / 1000));
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
  startCycleTask(/*first=*/true);
}

void loop() {
  ulTaskNotifyTake(pdTRUE, portMAX_DELAY);  // the press's task has ended
  const int ready = clientReadySeconds();
  if (waitReady(ready >= 0 ? ready * 1000UL : kDefaultReadyMs)) {
    nextFromReady = true;
    startCycleTask(/*first=*/false);
    return;
  }
#ifdef VOISTIC_BOARD_C6
  const int seconds = clientAwakeSeconds();
  const uint32_t limitMs = seconds >= 0 ? seconds * 1000UL : kDefaultStayConnectedMs;
  if (limitMs > 0 && waitConnected(limitMs)) {
    startCycleTask(/*first=*/false);  // waitConnected says how it starts
    return;
  }
#endif
  WiFi.disconnect(/*wifioff=*/true);
  sleepNow();
}
