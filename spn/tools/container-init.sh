#!/bin/sh

DATA="/data"
START="/data/portmaster-start"
INIT_START="/init/portmaster-start"

# Set safe shell options.
set -euf -o pipefail

# Check if data dir is mounted.
if [ ! -d $DATA ]; then
  echo "Nothing mounted at $DATA, aborting."
  exit 1
fi

# Copy init start to correct location, if not available.
if [ ! -f $START ]; then
  cp $INIT_START $START
fi

# Download updates.
echo "running: $START update --data /data --intel-only"
$START update --data /data --intel-only

# Remove PID file, which could have been left after a crash.
rm -f $DATA/hub-lock.pid

# Always start the SPN Hub with the updated main start binary.
echo "running: $START hub --data /data -- $@"
$START hub --data /data -- $@
