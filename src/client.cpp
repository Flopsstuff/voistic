#include "client.h"
#include "leds.h"

#include <ESPmDNS.h>
#include <HTTPClient.h>
#include <Preferences.h>
#include <WiFi.h>
#include <WiFiUdp.h>
#include <Update.h>
#include <esp_mac.h>
#include <mbedtls/md.h>
#include <mbedtls/sha256.h>
#include <mdns.h>

namespace {

constexpr char kNamespace[] = "voistic";
constexpr char kKeyKey[] = "key";
constexpr char kHostKey[] = "host";
constexpr char kPortKey[] = "port";
constexpr char kSeqKey[] = "seq";
constexpr char kBrightnessKey[] = "brightness";

constexpr char kSignatureHeader[] = "X-Voistic-Sig";
constexpr int kKeyBytes = 32;
constexpr int kSendAttempts = 3;
constexpr uint32_t kConnectTimeoutMs = 1500;
constexpr uint16_t kReadTimeoutMs = 2000;
constexpr uint32_t kAudioAnswerSeconds = 3;
constexpr uint32_t kDownloadStallMs = 10000;
constexpr size_t kDownloadChunk = 4096;
constexpr size_t kHashBytes = 32;
constexpr size_t kAudioChunkPrefix = 8;  // "ffff\r\n"
constexpr uint32_t kQueryTimeoutMs = 3000;
constexpr size_t kMaxServers = 8;
constexpr uint32_t kPairRetryMs = 1000;
constexpr uint32_t kPairPollMs = 50;

// The address is cached, because an mDNS query takes seconds.
struct ServerAddress {
  String host;
  uint16_t port = 0;
};

Preferences prefs;

WiFiClient audioSocket;
uint64_t audioSeq = 0;
uint8_t audioKey[kKeyBytes];
uint64_t updateSeq = 0;

void openPrefs() {
  static bool opened = false;
  if (!opened) {
    opened = prefs.begin(kNamespace, /*readOnly=*/false);
  }
}

String toHex(const uint8_t* data, size_t length) {
  String hex;
  hex.reserve(length * 2);
  for (size_t i = 0; i < length; ++i) {
    char byte[3];
    snprintf(byte, sizeof(byte), "%02x", data[i]);
    hex += byte;
  }
  return hex;
}

bool fromHex(const String& hex, uint8_t* out, size_t length) {
  if (hex.length() != length * 2) {
    return false;
  }
  for (size_t i = 0; i < length; ++i) {
    char byte[3] = {hex[i * 2], hex[i * 2 + 1], '\0'};
    char* end = nullptr;
    out[i] = static_cast<uint8_t>(strtoul(byte, &end, 16));
    if (end != byte + 2) {
      return false;
    }
  }
  return true;
}

String sign(const uint8_t* key, const String& message) {
  uint8_t mac[32];
  mbedtls_md_hmac(mbedtls_md_info_from_type(MBEDTLS_MD_SHA256), key, kKeyBytes,
                  reinterpret_cast<const uint8_t*>(message.c_str()), message.length(), mac);
  return toHex(mac, sizeof(mac));
}

ServerAddress foundServers[kMaxServers];

// A server lists every address it has, a virtual adapter's among them, and only
// one on this button's subnet can be reached.
bool isOnOurSubnet(uint32_t address) {
  const uint32_t mask = WiFi.subnetMask();
  return (address & mask) == (static_cast<uint32_t>(WiFi.localIP()) & mask);
}

// Asks mDNS who's there. Several servers can share a network and only one of
// them is this button's, so a caller may have to try each. This goes to ESP-IDF
// because MDNS.IP() only gives the first address of each server.
int findServers() {
  static bool started = false;
  if (!started) {
    started = MDNS.begin(clientDeviceId().c_str());
  }
  mdns_result_t* results = nullptr;
  if (mdns_query_ptr("_voistic", "_tcp", kQueryTimeoutMs, kMaxServers, &results) != ESP_OK) {
    return 0;
  }
  int count = 0;
  for (const mdns_result_t* result = results; result != nullptr && count < kMaxServers;
       result = result->next) {
    for (const mdns_ip_addr_t* address = result->addr; address != nullptr; address = address->next) {
      if (address->addr.type != ESP_IPADDR_TYPE_V4) {
        continue;
      }
      const IPAddress ip(address->addr.u_addr.ip4.addr);
      Serial.printf("mdns: %s:%u\n", ip.toString().c_str(), result->port);
      if (isOnOurSubnet(address->addr.u_addr.ip4.addr)) {
        foundServers[count].host = ip.toString();
        foundServers[count].port = result->port;
        ++count;
        break;
      }
    }
  }
  mdns_query_results_free(results);
  Serial.printf("mdns: %d servers\n", count);
  return count;
}

ServerAddress foundServer(int index) { return foundServers[index]; }

// Takes the skip-th server found, wrapping around, so that retries go on to the
// next one.
bool discover(ServerAddress& server, int skip = 0) {
  const int count = findServers();
  if (count <= 0) {
    return false;
  }
  server = foundServer(skip % count);
  return true;
}

void saveServer(const ServerAddress& server) {
  prefs.putString(kHostKey, server.host);
  prefs.putUShort(kPortKey, server.port);
}

int post(const ServerAddress& server, const char* path, const String& body, const String& signature,
         String& response, String& responseSignature) {
  WiFiClient socket;
  HTTPClient http;
  http.setConnectTimeout(kConnectTimeoutMs);
  http.setTimeout(kReadTimeoutMs);
  if (!http.begin(socket, server.host, server.port, path)) {
    return -1;
  }
  const char* collected[] = {kSignatureHeader};
  http.collectHeaders(collected, 1);
  http.addHeader("Content-Type", "application/json");
  if (signature.length() > 0) {
    http.addHeader(kSignatureHeader, signature);
  }

  const int code = http.POST(body);
  if (code > 0) {
    response = http.getString();
    responseSignature = http.header(kSignatureHeader);
  }
  http.end();
  return code;
}

// A forged or replayed answer must not look like success, so it's reported as
// no answer at all.
bool parseAnswer(const String& body, uint64_t seq, const uint8_t* key,
                 const String& signature, SendResult& result) {
  if (!signature.equalsIgnoreCase(sign(key, body))) {
    Serial.println("server: bad signature on the answer");
    return false;
  }
  if (body.indexOf("\"seq\":" + String(seq) + "}") < 0) {
    Serial.println("server: answer is for another press");
    return false;
  }
  if (body.indexOf("\"status\":\"ok\"") >= 0) {
    result = SendResult::Accepted;
  } else if (body.indexOf("\"status\":\"unbound\"") >= 0) {
    result = SendResult::Unbound;
  } else {
    result = SendResult::NoServer;
  }
  return true;
}

// The text of a string field in an answer this code has already verified.
String stringField(const String& body, const char* name) {
  const String opening = String("\"") + name + "\":\"";
  const int start = body.indexOf(opening);
  if (start < 0) {
    return "";
  }
  const int from = start + opening.length();
  return body.substring(from, body.indexOf('"', from));
}

size_t sizeField(const String& body, const char* name) {
  const String opening = String("\"") + name + "\":";
  const int start = body.indexOf(opening);
  return start < 0 ? 0 : body.substring(start + opening.length()).toInt();
}

bool tellServerUnpaired() {
  uint8_t key[kKeyBytes];
  if (!fromHex(prefs.getString(kKeyKey), key, kKeyBytes) || WiFi.status() != WL_CONNECTED) {
    return false;
  }
  ServerAddress server;
  server.host = prefs.getString(kHostKey);
  server.port = prefs.getUShort(kPortKey);

  const uint64_t seq = prefs.getULong64(kSeqKey, 0) + 1;
  const String body = "{\"device\":\"" + clientDeviceId() + "\",\"seq\":" + String(seq) + "}";
  String response;
  String responseSignature;
  const int code = post(server, "/v1/unpair", body, sign(key, body), response, responseSignature);
  Serial.printf("unpair: http %d\n", code);
  return code == HTTP_CODE_OK;
}

}  // namespace

String clientDeviceId() {
  uint8_t mac[6];
  esp_read_mac(mac, ESP_MAC_WIFI_STA);
  return "voistic-" + toHex(mac + 4, 2);
}

void clientWarmUp() {
  openPrefs();
  IPAddress host;
  if (host.fromString(prefs.getString(kHostKey))) {
    WiFiUDP udp;
    udp.beginPacket(host, prefs.getUShort(kPortKey));
    udp.write(0);
    udp.endPacket();
  }
}

void clientRestoreBrightness() {
  openPrefs();
  const float saved = prefs.getFloat(kBrightnessKey, -1.0f);
  if (saved >= 0.0f) {
    ledsSetBrightness(saved);
  }
}

void clientSaveBrightness(float brightness) {
  openPrefs();
  ledsSetBrightness(brightness);
  prefs.putFloat(kBrightnessKey, brightness);
}

bool clientIsPaired() {
  openPrefs();
  return prefs.isKey(kKeyKey);
}

SendResult clientSendPress(const char* gesture) {
  openPrefs();
  uint8_t key[kKeyBytes];
  if (!fromHex(prefs.getString(kKeyKey), key, kKeyBytes)) {
    return SendResult::NotPaired;
  }

  // Saved first, so a press lost to a reset can't reuse its number.
  const uint64_t seq = prefs.getULong64(kSeqKey, 0) + 1;
  prefs.putULong64(kSeqKey, seq);

  const String body = "{\"device\":\"" + clientDeviceId() + "\",\"gesture\":\"" + gesture +
                      "\",\"seq\":" + String(seq) + ",\"fw\":\"" FW_VERSION "\",\"rssi\":" +
                      String(WiFi.RSSI()) + "}";
  const String signature = sign(key, body);

  ServerAddress server;
  server.host = prefs.getString(kHostKey);
  server.port = prefs.getUShort(kPortKey);

  // Retries reuse the seq, so the server runs the action at most once.
  bool refused = false;
  int found = 0;
  for (int attempt = 0; attempt < kSendAttempts; ++attempt) {
    if (server.host.isEmpty() && !discover(server, found++)) {
      continue;
    }
    String response;
    String responseSignature;
    const int code = post(server, "/v1/press", body, signature, response, responseSignature);
    Serial.printf("press: seq %llu, attempt %d, http %d\n", seq, attempt + 1, code);

    if (code == HTTP_CODE_OK) {
      SendResult result;
      if (parseAnswer(response, seq, key, responseSignature, result)) {
        saveServer(server);
        return result;
      }
    } else if (code == HTTP_CODE_UNAUTHORIZED) {
      refused = true;  // maybe another computer's server, so this isn't final yet
    }
    server.host = "";  // it may have moved, ask mDNS next time
  }
  return refused ? SendResult::NotPaired : SendResult::NoServer;
}

UpdateCheck clientCheckUpdate(UpdateOffer& offer) {
  openPrefs();
  uint8_t key[kKeyBytes];
  if (!fromHex(prefs.getString(kKeyKey), key, kKeyBytes)) {
    return UpdateCheck::Failed;
  }
  ServerAddress server;
  server.host = prefs.getString(kHostKey);
  server.port = prefs.getUShort(kPortKey);
  if (server.host.isEmpty() && !discover(server)) {
    return UpdateCheck::Failed;
  }

  updateSeq = prefs.getULong64(kSeqKey, 0) + 1;
  prefs.putULong64(kSeqKey, updateSeq);
  const String body = "{\"device\":\"" + clientDeviceId() + "\",\"seq\":" + String(updateSeq) +
                      ",\"fw\":\"" FW_VERSION "\",\"rssi\":" + String(WiFi.RSSI()) + "}";
  String response;
  String responseSignature;
  const int code = post(server, "/v1/update", body, sign(key, body), response, responseSignature);
  Serial.printf("update: seq %llu, http %d\n", updateSeq, code);

  SendResult result;
  if (code != HTTP_CODE_OK || !parseAnswer(response, updateSeq, key, responseSignature, result)) {
    return UpdateCheck::Failed;
  }
  if (response.indexOf("\"status\":\"update\"") < 0) {
    return UpdateCheck::Current;
  }
  offer.version = stringField(response, "version");
  offer.sha256 = stringField(response, "sha256");
  offer.size = sizeField(response, "size");
  return offer.size > 0 && offer.sha256.length() == kHashBytes * 2 ? UpdateCheck::Offered
                                                                   : UpdateCheck::Failed;
}

bool clientDownloadUpdate(const UpdateOffer& offer) {
  uint8_t key[kKeyBytes];
  if (!fromHex(prefs.getString(kKeyKey), key, kKeyBytes)) {
    return false;
  }
  const String device = clientDeviceId();
  const String seq(updateSeq);
  const String path = "/v1/firmware?device=" + device + "&seq=" + seq;
  WiFiClient socket;
  HTTPClient http;
  http.setConnectTimeout(kConnectTimeoutMs);
  http.setTimeout(kReadTimeoutMs);
  if (!http.begin(socket, prefs.getString(kHostKey), prefs.getUShort(kPortKey), path)) {
    return false;
  }
  http.addHeader(kSignatureHeader, sign(key, "firmware|" + device + "|" + seq));
  const int code = http.GET();
  Serial.printf("firmware: %s, %u bytes, http %d\n", offer.version.c_str(),
                static_cast<unsigned>(offer.size), code);
  if (code != HTTP_CODE_OK || !Update.begin(offer.size)) {
    http.end();
    return false;
  }

  mbedtls_sha256_context context;
  mbedtls_sha256_init(&context);
  mbedtls_sha256_starts(&context, /*is224=*/0);

  static uint8_t chunk[kDownloadChunk];
  WiFiClient& stream = http.getStream();
  size_t received = 0;
  uint32_t lastDataMs = millis();
  while (received < offer.size && millis() - lastDataMs < kDownloadStallMs) {
    const int available = stream.available();
    if (available <= 0) {
      delay(1);
      continue;
    }
    const size_t wanted = min({static_cast<size_t>(available), sizeof(chunk), offer.size - received});
    const size_t got = stream.read(chunk, wanted);
    if (Update.write(chunk, got) != got) {
      break;
    }
    mbedtls_sha256_update(&context, chunk, got);
    received += got;
    lastDataMs = millis();
  }
  http.end();

  uint8_t hash[kHashBytes];
  mbedtls_sha256_finish(&context, hash);
  mbedtls_sha256_free(&context);
  if (received != offer.size || !toHex(hash, kHashBytes).equalsIgnoreCase(offer.sha256)) {
    Serial.printf("firmware: bad download, %u bytes\n", static_cast<unsigned>(received));
    Update.abort();
    return false;
  }
  return Update.end();
}

bool clientAudioBegin(uint32_t rate) {
  openPrefs();
  if (!fromHex(prefs.getString(kKeyKey), audioKey, kKeyBytes)) {
    return false;
  }
  ServerAddress server;
  server.host = prefs.getString(kHostKey);
  server.port = prefs.getUShort(kPortKey);
  if (server.host.isEmpty() && !discover(server)) {
    return false;
  }

  audioSeq = prefs.getULong64(kSeqKey, 0) + 1;
  prefs.putULong64(kSeqKey, audioSeq);

  const String device = clientDeviceId();
  const String seq(audioSeq);
  const String message = "audio|" + device + "|" + seq + "|" + String(rate);
  if (!audioSocket.connect(server.host.c_str(), server.port, kConnectTimeoutMs)) {
    Serial.println("audio: can't connect");
    return false;
  }
  audioSocket.setNoDelay(true);
  const String request = "POST /v1/audio?device=" + device + "&seq=" + seq + "&rate=" + String(rate) +
                         "&rssi=" + String(WiFi.RSSI()) + " HTTP/1.1\r\nHost: " + server.host + "\r\nContent-Type: audio/L16\r\n" +
                         "Transfer-Encoding: chunked\r\n" + kSignatureHeader + ": " +
                         sign(audioKey, message) + "\r\n\r\n";
  return audioSocket.print(request) == request.length();
}

bool clientAudioSend(const uint8_t* data, size_t length) {
  char prefix[kAudioChunkPrefix];
  const int prefixLength = snprintf(prefix, sizeof(prefix), "%x\r\n", static_cast<unsigned>(length));
  return audioSocket.write(reinterpret_cast<const uint8_t*>(prefix), prefixLength) ==
             static_cast<size_t>(prefixLength) &&
         audioSocket.write(data, length) == length && audioSocket.print("\r\n") == 2;
}

SendResult clientAudioEnd() {
  if (audioSocket.print("0\r\n\r\n") != 5) {
    return SendResult::NoServer;
  }
  audioSocket.setTimeout(kAudioAnswerSeconds);

  const String status = audioSocket.readStringUntil('\n');
  Serial.printf("audio: %s\n", status.c_str());
  const int code = status.substring(status.indexOf(' ') + 1).toInt();
  String signature;
  size_t length = 0;
  for (;;) {
    String line = audioSocket.readStringUntil('\n');
    line.trim();
    if (line.isEmpty()) {
      break;
    }
    const int colon = line.indexOf(':');
    const String value = line.substring(colon + 1);
    if (line.substring(0, colon).equalsIgnoreCase(kSignatureHeader)) {
      signature = value;
      signature.trim();
    } else if (line.substring(0, colon).equalsIgnoreCase("Content-Length")) {
      length = value.toInt();
    }
  }
  String body;
  while (body.length() < length) {
    const int c = audioSocket.read();
    if (c < 0 && !audioSocket.connected()) {
      break;
    }
    if (c >= 0) {
      body += static_cast<char>(c);
    }
  }
  audioSocket.stop();

  SendResult result = SendResult::NoServer;
  if (code == HTTP_CODE_OK) {
    parseAnswer(body, audioSeq, audioKey, signature, result);
  } else if (code == HTTP_CODE_UNAUTHORIZED) {
    result = SendResult::NotPaired;
  }
  return result;
}

bool clientPair(uint32_t timeoutMs, bool (*cancelled)()) {
  openPrefs();
  const String body = "{\"device\":\"" + clientDeviceId() + "\",\"fw\":\"" FW_VERSION "\"}";

  const uint32_t start = millis();
  while (millis() - start < timeoutMs) {
    const int count = findServers();
    // Only the server with `voistic pair` running says yes.
    for (int index = 0; index < count; ++index) {
      if (cancelled()) {
        return false;
      }
      const ServerAddress server = foundServer(index);
      String response;
      String unused;
      const int code = post(server, "/v1/pair", body, "", response, unused);
      Serial.printf("pair: %s:%u, http %d\n", server.host.c_str(), server.port, code);
      if (code != HTTP_CODE_OK) {
        continue;
      }

      const int keyAt = response.indexOf("\"key\":\"");
      if (keyAt < 0) {
        continue;
      }
      const String hex = response.substring(keyAt + 7, keyAt + 7 + kKeyBytes * 2);
      uint8_t key[kKeyBytes];
      if (!fromHex(hex, key, kKeyBytes)) {
        continue;
      }
      prefs.putString(kKeyKey, hex);
      prefs.putULong64(kSeqKey, 0);
      saveServer(server);
      return true;
    }
    for (uint32_t waited = 0; waited < kPairRetryMs; waited += kPairPollMs) {
      if (cancelled()) {
        return false;
      }
      delay(kPairPollMs);
    }
  }
  return false;
}

void clientRelease() {
  openPrefs();
  if (tellServerUnpaired()) {
    prefs.clear();
  }
}

void clientForget() {
  openPrefs();
  tellServerUnpaired();
  prefs.clear();
}
