# Testing Port17

## Simple Docker Setup

Run `run.sh` to start the docker compose test network.
Then, connect to the test network, by starting the core with the "test" spn map and the correct bootstrap file.

Run `stop.sh` to remove all docker resources again.

Setup Guide can be found in the directory.

## Advanced Setup with Shadow

For advanced testing we use [shadow](https://github.com/shadow/shadow).
The following section will help you set up shadow and will guide you how to test Port17 in a local Shadow environment.

### Setting up

Download the docker version from here: [https://security.cs.georgetown.edu/shadow-docker-images/shadow-standalone.tar.gz](https://security.cs.georgetown.edu/shadow-docker-images/shadow-standalone.tar.gz)

Then import the image into docker with `gunzip -c shadow-standalone.tar.gz | sudo docker load`.

### Running

Execute `sudo docker run -t -i -u shadow shadow-standalone /bin/bash` to start an interactive container with shadow.
