#!/bin/sh

# Get hostname.
HUBNAME=$HOSTNAME
if [ "$HUBNAME" = "" ]; then
  HUBNAME=$(cat /etc/hostname)
fi
export HUBNAME

# Read, process and write config.
cat /opt/shared/config-template.json | sed "s/\$HUBNAME/$HUBNAME/g" > /opt/data/config.json

# Get binary to start.
BIN=$(ls /opt/ | grep hub)

# Start Hub.
/opt/$BIN --data /opt/data --log trace --spn-map test --bootstrap-file /opt/shared/bootstrap.dsd --api-address 0.0.0.0:817 --devmode
