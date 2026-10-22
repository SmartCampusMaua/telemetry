#!/usr/bin/env bash
# Wraps `idf.py set-target` with friendly board names so you don't have to
# remember the exact ESP-IDF target string per board.
#
# `idf.py set-target` selects the chip toolchain/HAL/linker scripts before
# compilation even starts — it cannot be driven from device_config.h (that's
# just preprocessor macros evaluated after the target is already fixed).
# This script is the equivalent convenience at the build-invocation level;
# device_config.h then picks GPIO pins for whichever target you selected via
# CONFIG_IDF_TARGET_* (see platform/esp-idf/firmware/*/main/device_config.h).
#
# Usage: scripts/set-target.sh <obu|rsu|ecu> <wrover|s3|c6mini>
set -euo pipefail

usage() {
  echo "Usage: $0 <obu|rsu|ecu> <wrover|s3|c6mini>" >&2
  exit 1
}

[ $# -eq 2 ] || usage

FIRMWARE="$1"
BOARD="$2"

case "$FIRMWARE" in
  obu | rsu | ecu) ;;
  *)
    echo "error: unknown firmware '$FIRMWARE' (expected obu, rsu, or ecu)" >&2
    usage
    ;;
esac

case "$BOARD" in
  wrover) TARGET="esp32" ;;
  s3) TARGET="esp32s3" ;;
  c6mini) TARGET="esp32c6" ;;
  *)
    echo "error: unknown board '$BOARD' (expected wrover, s3, or c6mini)" >&2
    usage
    ;;
esac

if [ -z "${IDF_PATH:-}" ]; then
  echo "error: IDF_PATH is not set — source \$IDF_PATH/export.sh first" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIRMWARE_DIR="$REPO_ROOT/platform/esp-idf/firmware/$FIRMWARE"

echo "Setting $FIRMWARE target to $TARGET (board: $BOARD)..."
cd "$FIRMWARE_DIR"
idf.py set-target "$TARGET"
