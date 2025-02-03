#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )"

docker compose -p spn-test-simple stop
docker compose -p spn-test-simple rm

oldnet=$(docker network ls | grep spn-test-simple | cut -d" " -f1)
if [[ $oldnet != "" ]]; then
  docker network rm $oldnet
fi

if [[ -d "data/shared" ]]; then
  rm -r "data/shared"
fi
