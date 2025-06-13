#!/bin/bash

# This script builds the updatemgr binary for Portmaster.
# It retrieves the current version from git tags, builds the binary,
# and optionally compresses it using UPX if available.
# Usage: ./build.sh

set -e

# #####################################################################
# Initialize 'version' variables
# #####################################################################

# Get source repository
SOURCE="$( (git remote -v | cut -f2 | cut -d" " -f1 | head -n 1) || echo "unknown" )"

# Get build time
BUILD_TIME="$(date -u "+%Y-%m-%dT%H:%M:%SZ" || echo "unknown")"

# Get version from git tags
VERSION="$(git tag --points-at || true)"
if [ -z "${VERSION}" ]; then
    dev_version="$(git describe --tags --first-parent --abbrev=0 || true)"
    if [ -n "${dev_version}" ]; then
        VERSION="${dev_version}_dev_build"
    fi
fi
if [ -z "${VERSION}" ]; then
    VERSION="dev_build"
fi

echo "Source        : $SOURCE"
echo "Build Time    : $BUILD_TIME"
echo "Version       : $VERSION"

# Create cleaned version without 'v' prefix and without suffix
version_clean="$(echo "${VERSION}" | sed -E 's/^[vV]//' | sed -E 's/_.*$//')"
if echo "${version_clean}" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$' > /dev/null; then
    VERSION_SemVer="${version_clean}"
    echo "VERSION_SemVer: $VERSION_SemVer"
else
    echo "VERSION_SemVer: [Empty - not a valid SemVer in Git Tag] - !!! WARNING !!!"
fi

# #####################################################################
# Build updatemgr
# #####################################################################
echo ""
echo "Building updatemgr..."

mkdir -p dist
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w \
        -X github.com/safing/portmaster/base/info.version=${VERSION} \
        -X github.com/safing/portmaster/base/info.buildSource=${SOURCE} \
        -X github.com/safing/portmaster/base/info.buildTime=${BUILD_TIME}" \
        -o dist/updatemgr

echo "Build complete."
echo ""

# #####################################################################
# Check if UPX is installed and compress the binary
# #####################################################################
if command -v upx &> /dev/null; then
    echo "UPX is installed. UPX can reduce binary size."
    read -p "Do you want to compress the binary with UPX? [y/N] " use_upx
    
    if [[ $use_upx =~ ^[Yy]$ ]]; then
        echo "Compressing with UPX..."
        upx --best dist/updatemgr
        echo "Compression complete."
    else
        echo "Skipping UPX compression."
    fi
else
    echo "UPX is not installed. Skipping compression."
fi

echo ""
echo "Build script completed. The updatemgr binary is located in the 'dist' directory."