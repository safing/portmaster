#!/bin/bash

cd "$( dirname "${BASH_SOURCE[0]}" )"

MAIN_INTEL_FILE="intel-testnet.json"

if [[ ! -f $MAIN_INTEL_FILE ]]; then
  echo "missing $MAIN_INTEL_FILE"
  exit 1
fi

echo "if the containing directory cannot be created, you might need to adjust permissions, as nodes are run with root in test containers..."
echo "$ sudo chmod -R 777 data/hub*/updates"
echo "starting to update..."

for hubDir in data/hub*; do
  # Build destination path
  hubIntelFile="${hubDir}/updates/all/intel/spn/main-intel_v0-0-0.dsd"

  # Copy file
  mkdir -p "${hubDir}/updates/all/intel/spn"
  echo -n "J" > "$hubIntelFile"
  cat $MAIN_INTEL_FILE >> "$hubIntelFile"

  echo "updated $hubIntelFile"
done

if [[ -d /var/lib/portmaster ]]; then
  echo "updating intel for local portmaster installation..."

  portmasterSPNIntelFile="/var/lib/portmaster/updates/all/intel/spn/main-intel_v0-0-0.dsd"
  echo -n "J" > "$portmasterSPNIntelFile"
  cat $MAIN_INTEL_FILE >> "$portmasterSPNIntelFile"
  echo "updated $portmasterSPNIntelFile"
fi
