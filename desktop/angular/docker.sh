#!/bin/bash

# cd to script dir
baseDir="$( cd "$(dirname "$0")" && pwd )"
cd "$baseDir"

# get base dir for mounting
mnt="$( cd ../.. && pwd )"

# run container and start dev server
docker run                                \
    -ti                                   \
    --rm                                  \
    -v $mnt:/portmaster                   \
    -w /portmaster/desktop/angular        \
    -p 8081:8080                          \
    node:latest                           \
    npm start -- --host 0.0.0.0 --port 8080
