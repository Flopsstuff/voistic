// Fast Wi-Fi join. Credentials stay in the Wi-Fi driver's own NVS storage. The
// channel and BSSID of the last join are kept across deep sleep, which skips the
// scan: measured 0.5-0.9 s from wake to connected, against 3.7 s with a scan.

#pragma once

#include <cstdint>

// Starts joining the saved network. Returns false if none is saved.
bool linkStart();

// Drives the join and returns true once connected.
bool linkPoll(uint32_t nowMs);

// Saves a network and joins it.
void linkSave(const char* ssid, const char* password);

// Erases the saved network.
void linkForget();
