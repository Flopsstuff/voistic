// Wi-Fi setup without USB: the board opens its own access point with a page for
// choosing a network.

#pragma once

#include <cstdint>

// Serves the setup page until a network has been joined and saved, and returns
// true, or returns false after timeoutMs. The access point is open, so a Wi-Fi
// password crosses the air unencrypted during setup; the short timeout limits
// that exposure.
bool portalRun(uint32_t timeoutMs);
