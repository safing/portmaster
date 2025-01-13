# Setup Guide

1. Build SPN Hub

```
cd ../../../cmds/hub/
./build
```

2. Reset any previous state (for a fresh test)

```
./reset-databases.sh
```

3. Change compose file and config template as required

Files:
- `docker-compose.yml`
- `config-template.json`

4. Start test network

```
./run.sh
```

5. Option 1: Join as Hub

For testing just one Hub with a different build or config, you can simply use `./join.sh` to join the network with the most recently build hub binary.

6. Option 2: Join as Portmaster

For connecting to the SPN test network with Portmaster, execute portmaster like this:

```
sudo ../../../cmds/portmaster-core/portmaster-core --disable-shutdown-event --devmode --log debug --data /opt/safing/portmaster --spn-map test --bootstrap-file ./testdata/shared/bootstrap.dsd
```

Note:
This uses the same portmaster data and config as your installed version.
As the SPN Test net operates under a different ID ("test" instead of "main"), this will not pollute the SPN state of your installed Portmaster.

7. Stop the test net

This is important, as just stopping the `./run.sh` script will leave you with interfaces with public IPs!

```
./stop.sh
```
