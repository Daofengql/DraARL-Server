#!/bin/sh
set -eu

wget -q -O /dev/null http://127.0.0.1:9000/healthz

output="$(mktemp /tmp/draarl-broadcast-health-XXXXXX)"
trap 'rm -f "$output"' EXIT HUP INT TERM

dd if=/dev/zero bs=1920 count=1 2>/dev/null | \
    ffmpeg -nostdin -hide_banner -loglevel error \
      -f s16le -ar 16000 -ac 1 -i pipe:0 -t 0.06 \
      -map 0:a:0 -c:a libopus -application voip -frame_duration 60 \
      -f opus -y "$output"

test -s "$output"
test "$(ffprobe -v error -protocol_whitelist file,pipe \
    -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 \
    "$output")" = "opus"
