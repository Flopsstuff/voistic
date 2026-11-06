// Recognises gestures from timestamped button edges. Has no Arduino dependency,
// so it can be tested on a computer.
//
//   1, 2 or 3 short presses   Single, Double, Triple
//   hold, however long        Long, on release; cancelled if the link never came
//   5 short                   Update, which asks the server for a new firmware
//   5 short, then hold        Pair after 5 s, Reset from 15 s to 20 s, on release;
//                             held longer, it cancels at once
//
// The setup gestures hide behind the five presses so that a hold stays free for
// other uses, such as talking to a future board with a microphone: the feedback
// for Long means "recording", and releasing means "done". That needs a network,
// so a hold that ends before the link is up cancels instead.

#pragma once

#include <cstdint>

// Ignored means the input ended without meaning anything, so nothing will follow.
enum class Gesture : uint8_t { None, Ignored, Single, Double, Triple, Long, Update, Pair, Reset };

// What releasing the button would do now, for feedback while it's still down.
// LongWaiting and Armed are holds that can't take effect yet, so releasing cancels
// them: the first waits for the link, the second hasn't reached Pair.
enum class Hold : uint8_t { None, LongWaiting, Long, Armed, Pair, Reset };

class GestureDetector {
 public:
  static constexpr uint32_t kDebounceMs = 20;
  static constexpr uint32_t kMultiPressGapMs = 350;
  static constexpr uint32_t kLongMs = 400;
  static constexpr uint32_t kSetupPresses = 5;
  static constexpr uint32_t kPairMs = 5000;
  static constexpr uint32_t kResetMs = 15000;
  // The reset window ends here, so a change of mind is always possible: a hold
  // past it means nothing, and the button can be let go safely.
  static constexpr uint32_t kResetEndMs = 20000;

  // down is the button at startup. A press that woke the board counts even if
  // it ended before startup; a button already down after a plain reset doesn't.
  GestureDetector(bool down, bool wokenByPress, uint32_t nowMs);

  // Feeds a raw reading. Readings must arrive in time order.
  void update(uint32_t ms, bool down);

  // Tells the detector the link is up. It stays up as far as gestures go: a press
  // lost to a dropped link is the sender's to report.
  void linkUp() { linkUp_ = true; }

  // Advances time without a new reading.
  void tick(uint32_t nowMs);

  // Returns the finished gesture once. Later input is ignored.
  Gesture take();

  Hold hold(uint32_t nowMs) const;
  bool isDown() const { return down_; }
  uint32_t pressCount() const { return pressCount_; }

 private:
  void finish(Gesture gesture);
  void onPress(uint32_t ms);
  void onRelease(uint32_t ms);

  bool down_;
  bool raw_;
  uint32_t rawSinceMs_;
  bool counting_;  // false while a press that shouldn't count is still down
  uint32_t pressedAtMs_ = 0;
  uint32_t releasedAtMs_ = 0;
  uint32_t presses_ = 0;
  uint32_t pressCount_ = 0;
  Gesture result_ = Gesture::None;
  bool finished_ = false;
  bool linkUp_ = false;
};
