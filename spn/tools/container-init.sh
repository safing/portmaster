#!/bin/sh

DATA="/data"
START="/data/spn-hub"
INIT_START="/init/spn-hub"

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


# Remove PID file, which could have been left after a crash.
rm -f $DATA/hub-lock.pid

# Always start the SPN Hub with the updated main start binary.
echo "running: $START"
$START -- $@
