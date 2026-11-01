// The SPH0645 microphone of a button built with VOISTIC_MIC. It captures from the
// moment the board wakes into a buffer in PSRAM, so the start of a sentence isn't
// lost while the network comes up.

#pragma once

#include <cstddef>
#include <cstdint>

constexpr uint32_t kMicRate = 16000;  // 16-bit mono, what speech recognition wants

// Starts capturing. Later calls do nothing.
void micStart();

// Takes up to maxBytes of captured audio, oldest first, and returns how much.
size_t micRead(uint8_t* out, size_t maxBytes);

// Stops capturing. What was captured can still be read.
void micStop();
