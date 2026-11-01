#include "portal.h"

#include <DNSServer.h>
#include <WebServer.h>
#include <WiFi.h>

#include "client.h"

namespace {

constexpr uint16_t kDnsPort = 53;
constexpr uint16_t kHttpPort = 80;
constexpr uint32_t kJoinTimeoutMs = 15000;
constexpr uint32_t kHandOverMs = 1000;  // lets the last page reach the browser

WebServer web(kHttpPort);
DNSServer dns;

String networks;  // <option> list for the page
String joinFailedFor;
String submittedSsid;
String submittedPassword;
bool submitted = false;

String escapeHtml(const String& text) {
  String escaped;
  escaped.reserve(text.length());
  for (const char c : text) {
    switch (c) {
      case '&':
        escaped += "&amp;";
        break;
      case '<':
        escaped += "&lt;";
        break;
      case '>':
        escaped += "&gt;";
        break;
      case '"':
        escaped += "&quot;";
        break;
      default:
        escaped += c;
    }
  }
  return escaped;
}

void scanNetworks() {
  networks = "";
  const int count = WiFi.scanNetworks();
  for (int i = 0; i < count; ++i) {
    const String name = escapeHtml(WiFi.SSID(i));
    const String option = "<option>" + name + "</option>";
    if (name.length() > 0 && networks.indexOf(option) < 0) {
      networks += option;  // the scan is sorted, so the strongest comes first
    }
  }
  WiFi.scanDelete();
}

const char kPageStart[] PROGMEM =
    "<!doctype html><meta charset=utf-8>"
    "<meta name=viewport content='width=device-width,initial-scale=1'>"
    "<title>voistic</title>"
    "<style>body{font:17px system-ui;margin:2em auto;max-width:22em;padding:0 1em}"
    "select,input,button{box-sizing:border-box;font:inherit;margin:.3em 0 1em;padding:.6em;width:100%}"
    "label{display:block}.error{color:#b00020}</style>";

void showForm() {
  String page = FPSTR(kPageStart);
  page += "<h1>" + clientDeviceId() + "</h1>";
  if (joinFailedFor.length() > 0) {
    page += "<p class=error>Couldn't join " + escapeHtml(joinFailedFor) +
            ". Check the password.</p>";
  }
  page += "<form method=post action=/save>"
          "<label>Network<select name=ssid required>" + networks + "</select></label>"
          "<label>Password<input name=password autocapitalize=none autocomplete=off "
          "autocorrect=off spellcheck=false></label>"
          "<button>Connect</button></form>";
  web.send(200, "text/html", page);
}

void save() {
  submittedSsid = web.arg("ssid");
  submittedPassword = web.arg("password");
  if (submittedSsid.isEmpty()) {
    showForm();
    return;
  }
  String page = FPSTR(kPageStart);
  page += "<h1>Connecting</h1><p>Joining " + escapeHtml(submittedSsid) +
          ". Watch the button: slow purple means it worked. If this network "
          "reappears, reconnect to it and try again.</p>";
  web.send(200, "text/html", page);
  submitted = true;
}

// Any other address sends the browser to the form, which is what makes phones
// and laptops pop up the page by themselves.
void redirectToForm() {
  web.sendHeader("Location", "http://" + WiFi.softAPIP().toString() + "/");
  web.send(302, "text/plain", "");
}

void startAccessPoint() {
  WiFi.mode(WIFI_AP);
  WiFi.softAP(clientDeviceId().c_str());
  dns.start(kDnsPort, "*", WiFi.softAPIP());
  web.begin();
  Serial.printf("portal: open at %s\n", WiFi.softAPIP().toString().c_str());
}

void stopAccessPoint() {
  web.stop();
  dns.stop();
  WiFi.softAPdisconnect(/*wifioff=*/true);
}

bool join() {
  WiFi.mode(WIFI_STA);
  WiFi.begin(submittedSsid.c_str(), submittedPassword.c_str());  // saves on its own
  const uint32_t start = millis();
  while (millis() - start < kJoinTimeoutMs) {
    if (WiFi.status() == WL_CONNECTED) {
      return true;
    }
    delay(50);
  }
  WiFi.disconnect(/*wifioff=*/false, /*eraseap=*/true);
  return false;
}

}  // namespace

bool portalRun(uint32_t timeoutMs) {
  WiFi.mode(WIFI_STA);
  scanNetworks();

  web.on("/", HTTP_GET, showForm);
  web.on("/save", HTTP_POST, save);
  web.onNotFound(redirectToForm);

  const uint32_t start = millis();
  while (millis() - start < timeoutMs) {
    startAccessPoint();
    submitted = false;
    while (!submitted && millis() - start < timeoutMs) {
      dns.processNextRequest();
      web.handleClient();
      delay(2);
    }
    if (submitted) {
      const uint32_t handOver = millis();
      while (millis() - handOver < kHandOverMs) {
        web.handleClient();
        delay(2);
      }
    }
    stopAccessPoint();

    if (!submitted) {
      return false;
    }
    if (join()) {
      return true;
    }
    Serial.println("portal: join failed");
    joinFailedFor = submittedSsid;
  }
  return false;
}
