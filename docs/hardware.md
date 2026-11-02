# Hardware

The button with a microphone, board `voistic-9af4`. It sleeps, wakes on a press and, while held, records the voice and streams it to a server on the LAN. Build the firmware with `pio run -e mic`.

This page follows the build from loose parts to the printed case. There's also [a short video](images/vid.mp4) of it at work.

## Parts

| Part | Role |
|---|---|
| Seeed XIAO ESP32S3 Plus (16 MB flash, 8 MB PSRAM) | processor, Wi-Fi, battery charging |
| Adafruit IoT Button with NeoPixel BFF | button and colour pixel, sits on the XIAO |
| SPH0645, a digital MEMS microphone with I²S | voice |
| LiPo, 600 mAh | power |

Here they are before assembly, with the XIAO's antenna on the right.

![The parts laid out on a cutting mat: the XIAO with its antenna, the microphone, the button board and the battery](images/0.jpg)

PlatformIO has no board definition for the Plus, so the project uses `seeed_xiao_esp32s3` with the flash size overridden.

## Wiring

Only the microphone is wired to the XIAO:

| XIAO | Microphone | GPIO |
|---|---|---|
| 3V3 | 3V | |
| GND | GND | |
| D7 | BCLK | 44 |
| D8 | DOUT | 7 |
| D9 | LRCL | 8 |

The power wires cross: the XIAO has GND above 3V3, while the microphone has 3V above GND. SEL is soldered to GND on the microphone board, so the audio is in the left channel.

The IoT Button BFF mounts directly on the XIAO, back to back, with no separate wires. The button comes out on A2 (GPIO3) and the pixel on A3 (GPIO4).

The battery connects to the BAT+ and BAT− pads on the back of the XIAO.

The microphone board ends up sticking out past the USB connector, between the XIAO and the BFF.

![The button side of the stack on USB, with the microphone board at the end](images/1.jpg)

## Assembly

The boards go together into one stack, which lies next to the battery and antenna.

![The stacked boards next to the battery and antenna](images/2.jpg)

The first prototype held together with tape. The battery sits behind the stack, the antenna folds around it on the XIAO's side, and the USB connector stays free for charging.

![The XIAO side of the taped prototype, with the antenna wrapped around the battery](images/5.jpg)

On the other side are the button and the pixel, with the battery wires tucked in beside them.

![The button side of the taped prototype, with the battery wires and the microphone board](images/6.jpg)

Wrapped in white tape, with only the button showing, it was already usable day to day.

![The prototype in a hand, wrapped in white tape with its button showing](images/3.jpg)

## Case

The case started as a render, a small stick with one button.

![A concept render of the voice stick next to a laptop](images/hero.jpg)

It's now two printed parts, modelled as Onshape custom features, with the sources and the STLs in [`cad/`](../cad). The stack goes into a tube from the top, near the back wall. A plate on the cap then pushes it forward as the cap goes on, until the button comes out through its hole. The cap has a skirt all around, and bumps on its sides snap into dimples in the tube.

The first print, 28 × 20 mm with a 1.8 mm wall, held everything with room to spare, and its cap wouldn't stay on. The current one is 22 × 18 × 80 mm with a 1 mm wall. The tube prints standing and the cap roof down, both with no supports. On a 0.4 mm nozzle the wall takes two perimeters with a variable line width, such as Arachne.

![The printed case in a hand, with the button and the microphone slit on its front](images/7.jpg)

The pixel has no window of its own. It shines through the microphone slit, and the USB plug goes in through a window in the cap.

![The case in a hand in front of this repository, with the pixel glowing through the slit](images/8.jpg)

## Microphone

- It's read at 32 kHz, since as far as we know the SPH0645 doesn't run any slower. The board reduces this to 16 kHz, 16-bit mono.
- The data sits in the top 18 bits of a 32-bit slot.
- The signal has a DC offset of about 11% of full scale, removed by a high-pass filter on the board.
- The gain, +18 dB, is set in `src/mic.cpp`. In recordings, speech sits around -27 dBFS with peaks around -14 dBFS.
- The microphone starts with a loud burst, so the first 0.1 s is dropped and the next 0.2 s fades in.

Recording starts at the wake and goes into a 6 MB ring buffer in PSRAM, which holds over three minutes, so whatever is said before the connection opens isn't lost. One recording is limited to three minutes.

## Power

- The board is in deep sleep between presses, where the ESP32-S3 itself draws tens of microamps.
- The NeoPixel is powered from 3V3 all the time and draws current even when dark, so it probably sets the battery life. The estimate is 0.5-1 mA, or 25-50 days on 600 mAh. **Nobody has measured the sleep current**, which takes a multimeter.
- The charge level can't be read: `ADC_BAT` has no divider, and the charger status isn't wired to a GPIO.
- The red charge LED is driven by the charger chip, not by firmware.

## Quirks

- The pixel keeps its last colour for as long as it has power. Its data line has to be driven: with firmware that doesn't know about it, it can light up bright white.
- There's no USB port during sleep. To flash the board, hold the button, which keeps it awake.
- Opening the USB port resets the board.
