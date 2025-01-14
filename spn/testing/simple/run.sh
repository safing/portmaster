#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )"

realpath() {
    path=`eval echo "$1"`
    folder=$(dirname "$path")
    echo $(cd "$folder"; pwd)/$(basename "$path"); 
}

leftovers=$(docker ps -a | grep spn-test-simple | cut -d" " -f1)
if [[ $leftovers != "" ]]; then
  docker stop $leftovers
  docker rm $leftovers
fi

if [[ ! -f "../../../cmds/hub/hub" ]]; then
  echo "please build the hub cmd using cmds/hub/build"
  exit 1
fi

# Create variables.
SPN_TEST_BIN="$(realpath ../../../cmds/hub/hub)"
SPN_TEST_DATA_DIR="$(realpath ./testdata)"
if [[ ! -d "$SPN_TEST_DATA_DIR" ]]; then
  mkdir "$SPN_TEST_DATA_DIR"
fi
SPN_TEST_SHARED_DATA_DIR="$(realpath ./testdata/shared)"
if [[ ! -d "$SPN_TEST_SHARED_DATA_DIR" ]]; then
  mkdir "$SPN_TEST_SHARED_DATA_DIR"
fi

# Check if there is an old binary for testing.
SPN_TEST_OLD_BIN=$SPN_TEST_BIN
if [[ -f "./testdata/old-hub" ]]; then
  SPN_TEST_OLD_BIN="$(realpath ./testdata/old-hub)"
  echo "WARNING: running in hybrid mode with old version at $SPN_TEST_OLD_BIN"
fi

# Export variables
export SPN_TEST_BIN
export SPN_TEST_OLD_BIN
export SPN_TEST_DATA_DIR
export SPN_TEST_SHARED_DATA_DIR

# Copy files.
cp config-template.json ./testdata/shared/config-template.json
cp entrypoint.sh ./testdata/shared/entrypoint.sh
chmod 555 ./testdata/shared/entrypoint.sh

# Run!
docker compose -p spn-test-simple up --remove-orphans
