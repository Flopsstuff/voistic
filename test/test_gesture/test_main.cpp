#include <unity.h>

#include "gesture.h"

namespace {

using Clock = GestureDetector;

// A detector that starts after a wake press which is still down at 60 ms.
GestureDetector wokenDown() { return GestureDetector(true, true, 60); }

GestureDetector wokenDownOnline() {
  auto detector = wokenDown();
  detector.linkUp();
  return detector;
}

Gesture settle(GestureDetector& detector, uint32_t ms) {
  detector.tick(ms);
  return detector.take();
}

void assertGesture(Gesture expected, Gesture actual) {
  TEST_ASSERT_EQUAL_INT(static_cast<int>(expected), static_cast<int>(actual));
}

void assertHold(Hold expected, Hold actual) {
  TEST_ASSERT_EQUAL_INT(static_cast<int>(expected), static_cast<int>(actual));
}

// A press from down to up, then time for the debounce to pass.
void press(GestureDetector& detector, uint32_t downMs, uint32_t upMs) {
  detector.update(downMs, true);
  detector.update(upMs, false);
  detector.tick(upMs + Clock::kDebounceMs);
}

}  // namespace

void setUp() {}
void tearDown() {}

void test_single_waits_for_the_gap() {
  auto detector = wokenDown();
  detector.update(150, false);
  assertGesture(Gesture::None, settle(detector, 150 + Clock::kMultiPressGapMs - 1));
  assertGesture(Gesture::Single, settle(detector, 150 + Clock::kMultiPressGapMs));
}

void test_take_returns_a_gesture_once() {
  auto detector = wokenDown();
  detector.update(150, false);
  assertGesture(Gesture::Single, settle(detector, 1000));
  assertGesture(Gesture::None, settle(detector, 2000));
}

void test_press_released_before_startup_still_counts() {
  GestureDetector detector(false, true, 60);
  assertGesture(Gesture::Single, settle(detector, 60 + Clock::kMultiPressGapMs));
}

void test_second_press_after_early_release_makes_a_double() {
  GestureDetector detector(false, true, 60);
  press(detector, 132, 204);
  assertGesture(Gesture::Double, settle(detector, 204 + Clock::kMultiPressGapMs));
}

void test_double() {
  auto detector = wokenDown();
  detector.update(150, false);
  press(detector, 300, 400);
  assertGesture(Gesture::None, settle(detector, 400 + Clock::kMultiPressGapMs - 1));
  assertGesture(Gesture::Double, settle(detector, 400 + Clock::kMultiPressGapMs));
}

void test_gap_just_too_long_is_a_single() {
  auto detector = wokenDown();
  detector.update(150, false);
  detector.tick(150 + Clock::kMultiPressGapMs);
  press(detector, 150 + Clock::kMultiPressGapMs + 1, 700);
  assertGesture(Gesture::Single, settle(detector, 2000));
}

void test_triple_waits_for_the_gap() {
  auto detector = wokenDown();
  detector.update(150, false);
  press(detector, 300, 400);
  press(detector, 550, 650);
  assertGesture(Gesture::None, settle(detector, 650 + Clock::kMultiPressGapMs - 1));
  assertGesture(Gesture::Triple, settle(detector, 650 + Clock::kMultiPressGapMs));
}

// Five short presses starting from a wake press, the last released at 1000 ms.
GestureDetector fivePresses() {
  auto detector = wokenDown();
  detector.update(150, false);
  press(detector, 300, 400);
  press(detector, 500, 600);
  press(detector, 700, 800);
  press(detector, 900, 1000);
  return detector;
}

void test_four_presses_mean_nothing() {
  auto detector = wokenDown();
  detector.update(150, false);
  press(detector, 300, 400);
  press(detector, 500, 600);
  press(detector, 700, 800);
  assertGesture(Gesture::Ignored, settle(detector, 800 + Clock::kMultiPressGapMs));
}

void test_five_presses_without_a_hold_ask_for_an_update() {
  auto detector = fivePresses();
  assertGesture(Gesture::Update, settle(detector, 1000 + Clock::kMultiPressGapMs));
}

void test_six_presses_mean_nothing() {
  auto detector = fivePresses();
  press(detector, 1100, 1200);
  assertGesture(Gesture::Ignored, settle(detector, 1200 + Clock::kMultiPressGapMs));
}

void test_setup_hold_boundaries() {
  const uint32_t start = 1100;
  const struct {
    uint32_t heldMs;
    Gesture gesture;
  } cases[] = {
      {Clock::kPairMs - 1, Gesture::Ignored},
      {Clock::kPairMs, Gesture::Pair},
      {Clock::kResetMs - 1, Gesture::Pair},
      {Clock::kResetMs, Gesture::Reset},
  };
  for (const auto& test : cases) {
    auto detector = fivePresses();
    detector.update(start, true);
    detector.update(start + test.heldMs, false);
    assertGesture(test.gesture, settle(detector, start + test.heldMs + 1000));
  }
}

void test_setup_hold_reports_progress() {
  const uint32_t start = 1100;
  auto detector = fivePresses();
  detector.update(start, true);
  detector.tick(start + Clock::kDebounceMs);
  assertHold(Hold::Armed, detector.hold(start + Clock::kDebounceMs));
  assertHold(Hold::Armed, detector.hold(start + Clock::kPairMs - 1));
  assertHold(Hold::Pair, detector.hold(start + Clock::kPairMs));
  assertHold(Hold::Reset, detector.hold(start + Clock::kResetMs));
}

void test_bounce_is_filtered() {
  auto detector = wokenDown();
  detector.update(146, false);  // glitch seen on the real board
  detector.update(146, true);
  detector.update(324, false);
  detector.update(327, true);
  detector.update(328, false);
  detector.tick(328 + Clock::kDebounceMs);
  assertGesture(Gesture::Single, settle(detector, 328 + Clock::kMultiPressGapMs));
  TEST_ASSERT_EQUAL_UINT32(1, detector.pressCount());
}

void test_press_shorter_than_debounce_is_ignored() {
  GestureDetector detector(false, false, 0);
  detector.update(500, true);
  detector.update(510, false);
  assertGesture(Gesture::None, settle(detector, 3000));
}

void test_hold_boundaries() {
  const struct {
    uint32_t heldMs;
    Gesture gesture;
  } cases[] = {
      {Clock::kLongMs - 1, Gesture::Single},
      {Clock::kLongMs, Gesture::Long},
      {Clock::kResetMs * 10, Gesture::Long},  // a hold never turns into setup
  };
  for (const auto& test : cases) {
    GestureDetector detector(true, true, 100);
    detector.linkUp();
    detector.update(100 + test.heldMs, false);
    assertGesture(test.gesture, settle(detector, 100 + test.heldMs + 1000));
  }
}

void test_hold_reports_progress_while_down() {
  auto detector = wokenDownOnline();
  assertHold(Hold::None, detector.hold(60 + Clock::kLongMs - 1));
  assertHold(Hold::Long, detector.hold(60 + Clock::kLongMs));
  assertHold(Hold::Long, detector.hold(60 + Clock::kResetMs * 10));
}

void test_long_fires_on_release_not_at_the_threshold() {
  auto detector = wokenDownOnline();
  assertGesture(Gesture::None, settle(detector, 5000));
  detector.update(5000, false);
  assertGesture(Gesture::Long, settle(detector, 5100));
}

void test_hold_without_a_link_cancels() {
  auto detector = wokenDown();
  assertHold(Hold::None, detector.hold(60 + Clock::kLongMs - 1));
  assertHold(Hold::LongWaiting, detector.hold(60 + Clock::kLongMs));
  detector.update(3000, false);
  assertGesture(Gesture::Ignored, settle(detector, 3100));
}

void test_link_arriving_during_a_hold_makes_it_long() {
  auto detector = wokenDown();
  assertHold(Hold::LongWaiting, detector.hold(1000));
  detector.linkUp();
  assertHold(Hold::Long, detector.hold(1000));
  detector.update(2000, false);
  assertGesture(Gesture::Long, settle(detector, 2100));
}

void test_short_then_long_is_a_long() {
  auto detector = wokenDownOnline();
  detector.update(150, false);
  press(detector, 300, 3000);
  assertGesture(Gesture::Long, detector.take());
}

void test_button_held_through_reset_is_not_a_press() {
  GestureDetector detector(true, false, 170);
  assertHold(Hold::None, detector.hold(5000));
  detector.update(5000, false);
  assertGesture(Gesture::None, settle(detector, 6000));
  press(detector, 6100, 6200);
  assertGesture(Gesture::Single, settle(detector, 7000));
}

void test_counter_counts_presses_for_feedback() {
  auto detector = wokenDown();
  TEST_ASSERT_EQUAL_UINT32(1, detector.pressCount());
  detector.update(150, false);
  detector.tick(170);
  detector.update(300, true);
  detector.tick(320);
  TEST_ASSERT_EQUAL_UINT32(2, detector.pressCount());
}

void test_clock_wrap() {
  const uint32_t start = 0xFFFFFF00;
  GestureDetector detector(true, true, start);
  detector.update(start + 150, false);
  detector.update(start + 300, true);
  detector.update(start + 400, false);
  detector.tick(start + 420);
  assertGesture(Gesture::Double, settle(detector, start + 400 + Clock::kMultiPressGapMs));
}

int main() {
  UNITY_BEGIN();
  RUN_TEST(test_single_waits_for_the_gap);
  RUN_TEST(test_take_returns_a_gesture_once);
  RUN_TEST(test_press_released_before_startup_still_counts);
  RUN_TEST(test_second_press_after_early_release_makes_a_double);
  RUN_TEST(test_double);
  RUN_TEST(test_gap_just_too_long_is_a_single);
  RUN_TEST(test_triple_waits_for_the_gap);
  RUN_TEST(test_four_presses_mean_nothing);
  RUN_TEST(test_five_presses_without_a_hold_ask_for_an_update);
  RUN_TEST(test_six_presses_mean_nothing);
  RUN_TEST(test_setup_hold_boundaries);
  RUN_TEST(test_setup_hold_reports_progress);
  RUN_TEST(test_bounce_is_filtered);
  RUN_TEST(test_press_shorter_than_debounce_is_ignored);
  RUN_TEST(test_hold_boundaries);
  RUN_TEST(test_hold_reports_progress_while_down);
  RUN_TEST(test_long_fires_on_release_not_at_the_threshold);
  RUN_TEST(test_hold_without_a_link_cancels);
  RUN_TEST(test_link_arriving_during_a_hold_makes_it_long);
  RUN_TEST(test_short_then_long_is_a_long);
  RUN_TEST(test_button_held_through_reset_is_not_a_press);
  RUN_TEST(test_counter_counts_presses_for_feedback);
  RUN_TEST(test_clock_wrap);
  return UNITY_END();
}
