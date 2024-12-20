#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )"

realpath() {
    path=`eval echo "$1"`
    folder=$(dirname "$path")
    echo $(cd "$folder"; pwd)/$(basename "$path"); 
}

if [[ ! -f "../../client" ]]; then
  echo "please compile client.go in main directory (output: client)"
  exit 1
fi

bin_path="$(realpath ../../client)"
data_path="$(realpath ./testdata)"
if [[ ! -d "$data_path" ]]; then
  mkdir "$data_path"
fi
shared_path="$(realpath ./testdata/shared)"
if [[ ! -d "$shared_path" ]]; then
  mkdir "$shared_path"
fi

docker network ls | grep spn-simpletest-network >/dev/null 2>&1
if [[ $? -ne 0 ]]; then
  docker network create spn-simpletest-network --subnet 6.0.0.0/24
fi

docker run -ti --rm \
--name spn-simpletest-clientsim \
--network spn-simpletest-network \
-v $bin_path:/opt/client:ro \
-v $data_path/clientsim:/opt/data \
-v $shared_path:/opt/shared \
--entrypoint /opt/client \
toolset.safing.network/dev \
--data /opt/data \
--bootstrap-file /opt/shared/bootstrap.dsd \
--log trace $*