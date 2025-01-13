#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )"

realpath() {
    path=`eval echo "$1"`
    folder=$(dirname "$path")
    echo $(cd "$folder"; pwd)/$(basename "$path"); 
}

leftover=$(docker ps -a | grep spn-test-simple-me | cut -d" " -f1)
if [[ $leftover != "" ]]; then
  docker stop $leftover
  docker rm $leftover
fi

if [[ ! -f "../../../cmds/hub/hub" ]]; then
  echo "please build the hub cmd using cmds/hub/build"
  exit 1
fi

SPN_TEST_BIN="$(realpath ../../../cmds/hub/hub)"
SPN_TEST_DATA_DIR="$(realpath ./testdata)"
if [[ ! -d "$SPN_TEST_DATA_DIR" ]]; then
  mkdir "$SPN_TEST_DATA_DIR"
fi
SPN_TEST_SHARED_DATA_DIR="$(realpath ./testdata/shared)"
if [[ ! -d "$SPN_TEST_SHARED_DATA_DIR" ]]; then
  mkdir "$SPN_TEST_SHARED_DATA_DIR"
fi

docker run -ti \
--name spn-test-simple-me \
--hostname me \
--network spn-test-simple_default \
-v $SPN_TEST_BIN:/opt/hub_me:ro \
-v $SPN_TEST_DATA_DIR/me:/opt/data \
-v $SPN_TEST_SHARED_DATA_DIR:/opt/shared \
--entrypoint /opt/hub_me \
toolset.safing.network/dev \
--devmode --api-address 0.0.0.0:8081 \
--data /opt/data -log trace --spn-map test --bootstrap-file /opt/shared/bootstrap.dsd
