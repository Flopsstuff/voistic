// The microphone of a button built with VOISTIC_MIC: an SPH0645 on I²S on the
// XIAO (mic.cpp), or an analog SEN0487 on the ADC of the IoT Button V2
// (mic_adc.cpp). It captures from the moment the board wakes into a buffer, so
// the start of a sentence isn't lost while the network comes up.

#pragma once

#include <cstddef>
#include <cstdint>

constexpr uint32_t kMicRate = 16000;  // 16-bit mono, what speech recognition wants

// Starts capturing. Calls while capturing do nothing.
void micStart();

// Takes up to maxBytes of captured audio, oldest first, and returns how much.
size_t micRead(uint8_t* out, size_t maxBytes);

// Stops capturing. What was captured can still be read.
void micStop();

// Drops what was captured so far, so the next read starts from now. Capturing
// goes on.
void micDiscard();

// Whether the capture has fully stopped and let go of the ADC or I²S.
bool micIdle();
