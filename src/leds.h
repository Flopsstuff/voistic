// Light feedback. A background task draws it, so animations keep running while
// the main loop waits on the network. The XIAO has one NeoPixel. The IoT Button
// V2 has an RGB LED, which shows the same colours, and a red/blue LED beside
// it for what goes on in the background.

#pragma once

#include <cstdint>

enum class Led : uint8_t {
  Off,
  Press1,       // first press
  Press2,       // second press
  Press3,       // third press
  Press4,       // fourth press and later
  HoldWaiting,  // long press recognised, no link yet
  HoldLong,     // recording, releasing sends the long gesture
  HoldArmed,    // five presses seen, keep holding
  HoldPair,     // release now for pairing
  HoldReset,    // release now to forget Wi-Fi and the server
  Accepted,     // the server took the press
  Failed,       // anything that went wrong
  Updating,     // taking or checking a new firmware
  Portal,       // waiting for Wi-Fi setup on the board's own network
  Pairing,      // looking for a server in pairing mode
  Paired,       // pairing worked
  BatteryLow,   // warning before sleep, only where the battery can be measured
};

void ledsBegin();

// Shows a state, at half brightness if asked. Setting the current state again
// doesn't restart it.
void ledsSet(Led led, bool half = false);

// Scales every colour on the pixel or the RGB LED, from 0 to 1.
void ledsSetBrightness(float brightness);

// Whether a state that plays once, such as Accepted, has played out.
bool ledsDone();

// The brightness the states are drawn at, from 0 to 1.
float ledsBrightness();

// Blocks until a state with a fixed length has finished.
void ledsWait();

// Turns the lights off and stops the task, for deep sleep.
void ledsStop();
