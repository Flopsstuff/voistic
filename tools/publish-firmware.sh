#!/bin/bash
# Builds a firmware and offers it to a button, which takes it on 5 presses.
#
#   tools/publish-firmware.sh mic voistic-9af4
set -euo pipefail

environment="${1:?usage: publish-firmware.sh <pio environment> <device>}"
device="${2:?usage: publish-firmware.sh <pio environment> <device>}"

cd "$(dirname "$0")/.."
pio run -e "$environment"
server/bin/voistic firmware "$device" ".pio/build/$environment/firmware.bin" \
  "$(cat ".pio/build/$environment/version.txt")"
