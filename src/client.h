// Talks to the voistic server: signed presses, pairing and finding it over mDNS.

#pragma once

#include <Arduino.h>

enum class SendResult { Accepted, Unbound, NoServer, NotPaired };

// The id of this button, such as voistic-8cbc.
String clientDeviceId();

// Applies the saved pixel brightness, if one was ever saved.
void clientRestoreBrightness();

// Sets the pixel brightness and saves it.
void clientSaveBrightness(float brightness);

// Reports whether this button has a key from a server.
bool clientIsPaired();

// Sends the server one throwaway packet, so its address is resolved before a
// gesture needs it. Sleep clears the ARP table, and the server hears the query
// only when its own Wi-Fi power saving lets it, which delayed the connection by
// 100-400 ms. Fire and forget, so call it as soon as Wi-Fi is up.
void clientWarmUp();

// Sends a gesture and waits for the signed answer.
SendResult clientSendPress(const char* gesture);

// A firmware the server wants this button to run.
struct UpdateOffer {
  String version;
  String sha256;
  size_t size = 0;
};

enum class UpdateCheck { Current, Offered, Failed };

// Asks the server for a firmware. An answer of any kind also proves that this
// firmware can reach the server.
UpdateCheck clientCheckUpdate(UpdateOffer& offer);

// Fetches an offered firmware into the free slot and checks its hash. It runs
// from the next restart.
bool clientDownloadUpdate(const UpdateOffer& offer);

// Opens a recording on the server, for 16-bit mono audio at rate.
bool clientAudioBegin(uint32_t rate);

// Sends a piece of the recording. Returns false if the connection failed.
bool clientAudioSend(const uint8_t* data, size_t length);

// Ends the recording and waits for the signed answer.
SendResult clientAudioEnd();

// Looks for a server that accepts this button and takes a key from it. Blocks
// for up to timeoutMs.
bool clientPair(uint32_t timeoutMs);

// Gives up the key before pairing again. The key stays if the server can't be
// reached, so a failed attempt doesn't cost a working button.
void clientRelease();

// Tells the server this button is giving up its key, then forgets the key and
// the server. The server is told on a best-effort basis: if it can't be reached
// it keeps the old key until `voistic forget`.
void clientForget();
