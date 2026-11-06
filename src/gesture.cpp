#include "gesture.h"

GestureDetector::GestureDetector(bool down, bool wokenByPress, uint32_t nowMs)
    : down_(down), raw_(down), rawSinceMs_(nowMs), counting_(!down || wokenByPress) {
  if (!wokenByPress) {
    return;
  }
  pressCount_ = 1;
  if (down) {
    pressedAtMs_ = nowMs;
  } else {
    presses_ = 1;
    releasedAtMs_ = nowMs;
  }
}

void GestureDetector::update(uint32_t ms, bool down) {
  tick(ms);
  if (down != raw_) {
    raw_ = down;
    rawSinceMs_ = ms;
  }
}

void GestureDetector::tick(uint32_t nowMs) {
  if (raw_ != down_ && nowMs - rawSinceMs_ >= kDebounceMs) {
    down_ = raw_;
    if (down_) {
      onPress(rawSinceMs_);
    } else {
      onRelease(rawSinceMs_);
    }
  }

  if (down_ && counting_ && presses_ == kSetupPresses && nowMs - pressedAtMs_ >= kResetEndMs) {
    finish(Gesture::Ignored);  // held past the reset window, which cancels
  }

  if (!down_ && presses_ > 0 && nowMs - releasedAtMs_ >= kMultiPressGapMs) {
    switch (presses_) {
      case 1:
        finish(Gesture::Single);
        break;
      case 2:
        finish(Gesture::Double);
        break;
      case 3:
        finish(Gesture::Triple);
        break;
      case kSetupPresses:
        finish(Gesture::Update);
        break;
      default:
        finish(Gesture::Ignored);
    }
  }
}

Gesture GestureDetector::take() {
  const Gesture gesture = result_;
  result_ = Gesture::None;
  return gesture;
}

Hold GestureDetector::hold(uint32_t nowMs) const {
  if (finished_ || !down_ || !counting_) {
    return Hold::None;
  }
  const uint32_t held = nowMs - pressedAtMs_;
  if (presses_ != kSetupPresses) {
    if (held < kLongMs) {
      return Hold::None;
    }
    return linkUp_ ? Hold::Long : Hold::LongWaiting;
  }
  if (held >= kResetMs) {
    return Hold::Reset;
  }
  return held >= kPairMs ? Hold::Pair : Hold::Armed;
}

void GestureDetector::finish(Gesture gesture) {
  if (!finished_) {
    finished_ = true;
    result_ = gesture;
  }
}

void GestureDetector::onPress(uint32_t ms) {
  counting_ = true;
  pressedAtMs_ = ms;
  if (!finished_) {
    ++pressCount_;
  }
}

void GestureDetector::onRelease(uint32_t ms) {
  if (!counting_) {
    return;
  }
  const uint32_t held = ms - pressedAtMs_;
  if (held < kLongMs) {
    ++presses_;
    releasedAtMs_ = ms;
  } else if (presses_ != kSetupPresses) {
    finish(linkUp_ ? Gesture::Long : Gesture::Ignored);  // no link, so nothing was recorded
  } else if (held >= kResetMs) {
    finish(Gesture::Reset);
  } else if (held >= kPairMs) {
    finish(Gesture::Pair);
  } else {
    finish(Gesture::Ignored);  // let go too early, which cancels
  }
}
