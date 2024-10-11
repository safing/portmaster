VERSION --arg-scope-and-set --global-cache 0.8

ARG --global go_version = 1.22
ARG --global node_version = 18
ARG --global rust_version = 1.79
ARG --global tauri_version = "2.0.1"
ARG --global golangci_lint_version = 1.57.1

ARG --global go_builder_image = "golang:${go_version}-alpine"
ARG --global node_builder_image = "node:${node_version}"
ARG --global rust_builder_image = "rust:${rust_version}-bookworm"
ARG --global work_image = "alpine"

ARG --global outputDir = "./dist"

# Architectures:
# The list of rust targets we support. They will be automatically converted
# to GOOS, GOARCH and GOARM when building go binaries. See the +RUST_TO_GO_ARCH_STRING
# helper method at the bottom of the file.
#
# Linux:
# x86_64-unknown-linux-gnu
# aarch64-unknown-linux-gnu
# armv7-unknown-linux-gnueabihf
# arm-unknown-linux-gnueabi
#
# Windows:
# x86_64-pc-windows-gnu
# aarch64-pc-windows-gnu
#
# Mac:
# x86_64-apple-darwin
# aarch64-apple-darwin

# Import the earthly rust lib since it already provides some useful
# build-targets and methods to initialize the rust toolchain.
IMPORT github.com/earthly/lib/rust:3.0.2 AS rust

build:
    # Build all Golang binaries:
    # ./dist/linux_amd64/portmaster-core
    # ./dist/linux_amd64/portmaster-start
    # ./dist/linux_arm64/portmaster-core
    # ./dist/linux_arm64/portmaster-start
    # ./dist/windows_amd64/portmaster-core.exe
    # ./dist/windows_amd64/portmaster-start.exe
    # ./dist/windows_arm64/portmaster-core.exe
    # ./dist/windows_arm64/portmaster-start.exe
    BUILD +go-build --GOOS="linux"   --GOARCH="amd64"
    BUILD +go-build --GOOS="linux"   --GOARCH="arm64"
    BUILD +go-build --GOOS="windows" --GOARCH="amd64"
    BUILD +go-build --GOOS="windows" --GOARCH="arm64"

    # Build the Angular UI:
    # ./dist/all/portmaster-ui.zip
    BUILD +angular-release

    # Build Tauri app binaries:
    # ./dist/linux_amd64/portmaster-app
    # ./dist/windows_amd64/portmaster-app
    BUILD +tauri-build --target="x86_64-unknown-linux-gnu"
    BUILD +tauri-build --target="x86_64-pc-windows-gnu"

    # TODO(vladimir): Build bundles
    # ./dist/linux_amd64/Portmaster-0.1.0-1.x86_64.rpm
    # ./dist/linux_amd64/Portmaster_0.1.0_amd64.deb
    # Bild Tauri bundle for Windows:

    # Build UI assets:
    # ./dist/all/assets.zip
    BUILD +assets

go-ci:
    BUILD +go-build --GOOS="linux"   --GOARCH="amd64"
    BUILD +go-build --GOOS="linux"   --GOARCH="arm64"
    BUILD +go-build --GOOS="windows" --GOARCH="amd64"
    BUILD +go-build --GOOS="windows" --GOARCH="arm64"
    BUILD +go-test

angular-ci:
    BUILD +angular-release

tauri-ci:
    BUILD +tauri-build --target="x86_64-unknown-linux-gnu"
    BUILD +tauri-build --target="x86_64-pc-windows-gnu"

kext-ci:
    BUILD +kext-build

release:
    LOCALLY

    IF ! git diff --quiet
        RUN echo -e "\033[1;31m Refusing to release a dirty git repository. Please commit your local changes first! \033[0m" ; exit 1
    END

    BUILD +build

go-deps:
    FROM ${go_builder_image}
    WORKDIR /go-workdir

    # We need the git cli to extract version information for go-builds
    RUN apk add git

    # These cache dirs will be used in later test and build targets
    # to persist cached go packages.
    #
    # NOTE: cache only gets persisted on successful builds. A test
    # failure will prevent the go cache from being persisted.
    ENV GOCACHE = "/.go-cache"
    ENV GOMODCACHE = "/.go-mod-cache"

    # Copying only go.mod and go.sum means that the cache for this
    # target will only be busted when go.mod/go.sum change. This
    # means that we can cache the results of 'go mod download'.
    COPY go.mod .
    COPY go.sum .
    RUN go mod download

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

go-base:
    FROM +go-deps

    # Copy the full repo, as Go embeds whether the state is clean.
    COPY . .

    LET version = "$(git tag --points-at || true)"
    IF [ -z "${version}" ]
        LET dev_version = "$(git describe --tags --first-parent --abbrev=0 || true)"
        IF [ -n "${dev_version}" ]
            SET version = "${dev_version}_dev_build"
        END
    END
    IF [ -z "${version}" ]
        SET version = "dev_build"
    END
    ENV VERSION="${version}"
    RUN echo "Version: $VERSION"

    LET source = $( ( git remote -v | cut -f2 | cut -d" " -f1 | head -n 1 ) || echo "unknown" )
    ENV SOURCE="${source}"
    RUN echo "Source: $SOURCE"

    LET build_time = $(date -u "+%Y-%m-%dT%H:%M:%SZ" || echo "unknown")
    ENV BUILD_TIME = "${build_time}"
    RUN echo "Build Time: $BUILD_TIME"

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

# updates all go dependencies and runs go mod tidy, saving go.mod and go.sum locally.
go-update-deps:
    FROM +go-base

    RUN go get -u ./..
    RUN go mod tidy
    SAVE ARTIFACT --keep-ts go.mod AS LOCAL go.mod
    SAVE ARTIFACT --keep-ts --if-exists go.sum AS LOCAL go.sum

# mod-tidy runs 'go mod tidy', saving go.mod and go.sum locally.
mod-tidy:
    FROM +go-base

    RUN go mod tidy
    SAVE ARTIFACT --keep-ts go.mod AS LOCAL go.mod
    SAVE ARTIFACT --keep-ts --if-exists go.sum AS LOCAL go.sum

# go-build runs 'go build ./cmds/...', saving artifacts locally.
# If --CMDS is not set, it defaults to building portmaster-start, portmaster-core and hub
go-build:
    FROM +go-base

    # Arguments for cross-compilation.
    ARG GOOS=linux
    ARG GOARCH=amd64
    ARG GOARM
    ARG CMDS=portmaster-start portmaster-core

    CACHE --sharing shared "$GOCACHE"
    CACHE --sharing shared "$GOMODCACHE"

    RUN mkdir /tmp/build

    # Fall back to build all binaries when none is specified.
    IF [ "${CMDS}" = "" ]
        LET CMDS=$(ls -1 "./cmds/")
    END

    # Build all go binaries from the specified in CMDS
    FOR bin IN $CMDS
        # Add special build options.
        IF [ "${GOOS}" = "windows" ] &&  [ "${bin}" = "portmaster-start" ]
            # Windows, portmaster-start
            ENV CGO_ENABLED = "1"
            ENV EXTRA_LD_FLAGS = "-H windowsgui"
        ELSE
            # Defaults
            ENV CGO_ENABLED = "0"
            ENV EXTRA_LD_FLAGS = ""
        END

        RUN --no-cache go build -ldflags="${EXTRA_LD_FLAGS} -X github.com/safing/portmaster/base/info.version=${VERSION} -X github.com/safing/portmaster/base/info.buildSource=${SOURCE} -X github.com/safing/portmaster/base/info.buildTime=${BUILD_TIME}" -o "/tmp/build/" ./cmds/${bin}
    END

    DO +GO_ARCH_STRING --goos="${GOOS}" --goarch="${GOARCH}" --goarm="${GOARM}"
    FOR bin IN $(ls -1 "/tmp/build/")
        SAVE ARTIFACT --keep-ts "/tmp/build/${bin}" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/${bin}"
    END

    SAVE ARTIFACT --keep-ts "/tmp/build/" ./output

spn-image:
    # Use minimal image as base.
    FROM alpine

    # Copy the static executable.
    COPY (+go-build/output/portmaster-start --GOARCH=amd64 --GOOS=linux --CMDS=portmaster-start) /init/portmaster-start

    # Copy the init script
    COPY spn/tools/container-init.sh /init.sh

    # Run the hub.
    ENTRYPOINT ["/init.sh"]

    # Get version.
    LET version = "$(/init/portmaster-start version --short | tr ' ' -)"
    RUN echo "Version: ${version}"

    # Save dev image
    SAVE IMAGE "spn:latest"
    SAVE IMAGE "spn:${version}"
    SAVE IMAGE "ghcr.io/safing/spn:latest"
    SAVE IMAGE "ghcr.io/safing/spn:${version}"

# Test one or more go packages.
# Test are always run as -short, as "long" tests require a full desktop system.
# Run `earthly +go-test` to test all packages
# Run `earthly +go-test --PKG="service/firewall"` to only test a specific package.
# Run `earthly +go-test --TESTFLAGS="-args arg1"` to add custom flags to go test (-args in this case)
go-test:
    FROM +go-base

    ARG GOOS=linux
    ARG GOARCH=amd64
    ARG GOARM
    ARG TESTFLAGS
    ARG PKG="..."

    CACHE --sharing shared "$GOCACHE"
    CACHE --sharing shared "$GOMODCACHE"

    FOR pkg IN $(go list -e "./${PKG}")
        RUN --no-cache go test -cover -short ${pkg} ${TESTFLAGS}
    END

go-test-all:
    FROM ${work_image}
    ARG --required architectures

    FOR arch IN ${architectures}
        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${arch}"
        BUILD +go-test --GOARCH="${GOARCH}" --GOOS="${GOOS}" --GOARM="${GOARM}"
    END

go-lint:
    FROM +go-base

    RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v${golangci_lint_version}
    RUN golangci-lint run -c ./.golangci.yml --timeout 15m --show-stats

# Builds portmaster-start, portmaster-core, hub and notifier for all supported platforms
go-release:
    FROM ${work_image}
    ARG --required architectures

    FOR arch IN ${architectures}
        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${arch}"

        IF [ -z GOARCH ]
            RUN echo "Failed to extract GOARCH for ${arch}"; exit 1
        END

        IF [ -z GOOS ]
            RUN echo "Failed to extract GOOS for ${arch}"; exit 1
        END

        BUILD +go-build --GOARCH="${GOARCH}" --GOOS="${GOOS}" --GOARM="${GOARM}"
    END

# Builds all binaries from the cmds/ folder for linux/windows AMD64
# Most utility binaries are never needed on other platforms.
go-build-utils:
    BUILD +go-build --CMDS="" --GOARCH=amd64 --GOOS=linux
    BUILD +go-build --CMDS="" --GOARCH=amd64 --GOOS=windows

# Prepares the angular project by installing dependencies
angular-deps:
    FROM ${node_builder_image}
    WORKDIR /app/ui

    RUN apt update && apt install zip

    COPY desktop/angular/package.json .
    COPY desktop/angular/package-lock.json .

    RUN npm install

# Copies the UI folder into the working container
# and builds the shared libraries in the specified configuration (production or development)
angular-base:
    FROM +angular-deps
    ARG configuration="production"

    COPY desktop/angular/ .
    # Remove symlink and copy assets directly.
    RUN rm ./assets
    COPY assets/data ./assets

    IF [ "${configuration}" = "production" ]
        RUN --no-cache npm run build-libs
    ELSE
        RUN --no-cache npm run build-libs:dev
    END

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

# Build an angualr project, zip it and save artifacts locally
angular-project:
    ARG --required project
    ARG --required dist
    ARG configuration="production"
    ARG baseHref="/"

    FROM +angular-base --configuration="${configuration}"

    IF [ "${configuration}" = "production" ]
        ENV NODE_ENV="production"
    END

    RUN --no-cache ./node_modules/.bin/ng build --configuration ${configuration} --base-href ${baseHref} "${project}"

    RUN --no-cache cwd=$(pwd) && cd "${dist}" && zip -r "${cwd}/${project}.zip" ./
    SAVE ARTIFACT --keep-ts "${dist}" "./output/${project}"

    # Save portmaster UI as local artifact.
    IF [ "${project}" = "portmaster" ]
        SAVE ARTIFACT --keep-ts "./${project}.zip" AS LOCAL ${outputDir}/all/${project}-ui.zip
        SAVE ARTIFACT --keep-ts "./${project}.zip" output/${project}.zip
    END

# Build the angular projects (portmaster-UI and tauri-builtin) in dev mode
angular-dev:
    BUILD +angular-project --project=portmaster --dist=./dist --configuration=development --baseHref=/ui/modules/portmaster/
    BUILD +angular-project --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=development --baseHref=/

# Build the angular projects (portmaster-UI and tauri-builtin) in production mode
angular-release:
    BUILD +angular-project --project=portmaster --dist=./dist --configuration=production --baseHref=/ui/modules/portmaster/
    BUILD +angular-project --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=production --baseHref=/

assets:
    FROM ${work_image}
    RUN apk add zip

    WORKDIR /app/assets
    COPY --keep-ts ./assets/data .
    RUN zip -r -9 -db assets.zip *

    SAVE ARTIFACT --keep-ts "assets.zip" AS LOCAL "${outputDir}/all/assets.zip"

# A base target for rust to prepare the build container
rust-base:
    FROM ${rust_builder_image}

    RUN apt-get update -qq

    # Tools and libraries required for cross-compilation
    RUN apt-get install --no-install-recommends -qq \
        autoconf \
        autotools-dev \
        libtool-bin \
        clang \
        cmake \
        bsdmainutils \
        gcc-multilib \
        linux-libc-dev \
        linux-libc-dev-amd64-cross \
        build-essential \
        curl \
        wget \
        file \
        libsoup-3.0-dev \
        libwebkit2gtk-4.1-dev \
        gcc-mingw-w64-x86-64 \
        zip

    # Install library dependencies for all supported architectures
    # required for succesfully linking.
    RUN apt-get install --no-install-recommends -qq \
        libsoup-3.0-0 \
        libwebkit2gtk-4.1-0 \
        libssl3 \
        libayatana-appindicator3-1 \
        librsvg2-bin \
        libgtk-3-0 \
        libjavascriptcoregtk-4.1-0  \
        libssl-dev \
        libayatana-appindicator3-dev \
        librsvg2-dev \
        libgtk-3-dev \
        libjavascriptcoregtk-4.1-dev  

    # Add some required rustup components
    RUN rustup component add clippy
    RUN rustup component add rustfmt

    DO rust+INIT --keep_fingerprints=true

    # For now we need tauri-cli 2.0.0 for bulding
    DO rust+CARGO --args="install tauri-cli --version ${tauri_version} --locked"

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

tauri-src:
    FROM +rust-base

    WORKDIR /app/tauri

    # --keep-ts is necessary to ensure that the timestamps of the source files
    # are preserved such that Rust's incremental compilation works correctly.
    COPY --keep-ts ./desktop/tauri/ .
    COPY assets/data ./../../assets/data
    COPY packaging ./../../packaging
    COPY (+angular-project/output/tauri-builtin --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=production --baseHref="/") ./../angular/dist/tauri-builtin

    WORKDIR /app/tauri/src-tauri

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

tauri-build:
    FROM +tauri-src

    ARG --required target

    ARG output=".*/release/([^\./]+|([^\./]+\.(dll|exe)))"
    DO +RUST_TO_GO_ARCH_STRING --rustTarget="${target}"
    RUN echo "GOOS=${GOOS} GOARCH=${GOARCH} GOARM=${GOARM} GO_ARCH_STRING=${GO_ARCH_STRING}"

    DO rust+SET_CACHE_MOUNTS_ENV
    RUN rustup target add "${target}"
    RUN --mount=$EARTHLY_RUST_TARGET_CACHE cargo tauri build  --ci --target="${target}" --no-bundle
    DO rust+COPY_OUTPUT --output="${output}"

    # BUG(cross-compilation):
    #
    # The above command seems to correctly compile for all architectures we want to support but fails during
    # linking since the target libaries are not available for the requested platforms. Maybe we need to download
    # the, manually ...
    #
    # The earthly rust lib also has support for using cross-rs for cross-compilation but that fails due to the
    # fact that cross-rs base docker images used for building are heavily outdated (latest = ubunut:16.0, main = ubuntu:20.04)
    # which does not ship recent enough glib versions (our glib dependency needs glib>2.70 but ubunut:20.04 only ships 2.64)
    #
    # The following would use the CROSS function from the earthly lib, this 
    # DO rust+CROSS --target="${target}"

    RUN echo output: $(ls -R "target/${target}/release")

    # Binaries
    SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/portmaster" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/portmaster"
    SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/portmaster.exe" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/portmaster.exe"
    # SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/WebView2Loader.dll" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/WebView2Loader.dll"

    SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/portmaster" ./output/portmaster
    SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/portmaster.exe" ./output/portmaster.exe


tauri-release:
    FROM ${work_image}
    ARG --required architectures

    FOR arch IN ${architectures}
        BUILD +tauri-build --target="${arch}"
    END

tauri-lint:
    FROM +rust-base
    ARG target="x86_64-unknown-linux-gnu"

    WORKDIR /app
    # Copy static files that are embedded inside the executable.
    COPY --keep-ts ./assets ./assets

    # Copy all the rust code
    COPY --keep-ts ./desktop/tauri ./desktop/tauri

    # Create a empty ui dir so it will satisfy the build.
    RUN mkdir -p ./desktop/angular/dist/tauri-builtin

    SAVE IMAGE --cache-hint

    # Run the linter.
    WORKDIR /app/desktop/tauri/src-tauri
    RUN --mount=$EARTHLY_RUST_TARGET_CACHE cargo clippy --all-targets --all-features -- -D warnings

release-prep:
    FROM +rust-base

    # Linux specific
    COPY (+tauri-build/output/portmaster --target="x86_64-unknown-linux-gnu") ./output/binary/linux_amd64/portmaster
    COPY (+go-build/output/portmaster-core --GOARCH=amd64 --GOOS=linux --CMDS=portmaster-core) ./output/binary/linux_amd64/portmaster-core

    # Windows specific
    COPY (+tauri-build/output/portmaster.exe --target="x86_64-pc-windows-gnu") ./output/binary/windows_amd64/portmaster.exe
    COPY (+go-build/output/portmaster-core.exe --GOARCH=amd64 --GOOS=windows --CMDS=portmaster-core) ./output/binary/windows_amd64/portmaster-core.exe
    # TODO(vladimir): figure out a way to get the lastest release of the kext.
    RUN touch ./output/binary/windows_amd64/portmaster-kext.sys

    # All platforms
    COPY (+assets/assets.zip) ./output/binary/all/assets.zip
    COPY (+angular-project/output/portmaster.zip --project=portmaster --dist=./dist --configuration=production --baseHref=/ui/modules/portmaster/) ./output/binary/all/portmaster.zip

    # Intel
    # TODO(vladimir): figure out a way to download all latest intel data.
    RUN mkdir -p ./output/intel
    RUN wget -O ./output/intel/geoipv4.mmdb.gz "https://updates.safing.io/all/intel/geoip/geoipv4_v20240529-0-1.mmdb.gz" && \
        wget -O ./output/intel/geoipv6.mmdb.gz "https://updates.safing.io/all/intel/geoip/geoipv6_v20240529-0-1.mmdb.gz" 

    RUN touch "./output/intel/index.dsd"
    RUN touch "./output/intel/base.dsdl"
    RUN touch "./output/intel/intermediate.dsdl"
    RUN touch "./output/intel/urgent.dsdl"

    COPY (+go-build/output/updatemgr --GOARCH=amd64 --GOOS=linux --CMDS=updatemgr) ./updatemgr
    RUN ./updatemgr scan --dir "./output/binary" > ./output/binary/bin-index.json
    RUN ./updatemgr scan --dir "./output/intel" > ./output/intel/intel-index.json

    # Intel Extracted (needed for the installers)
    RUN mkdir -p ./output/intel_decompressed
    RUN cp ./output/intel/intel-index.json ./output/intel_decompressed/intel-index.json
    RUN gzip -dc ./output/intel/geoipv4.mmdb.gz > ./output/intel_decompressed/geoipv4.mmdb
    RUN gzip -dc ./output/intel/geoipv6.mmdb.gz > ./output/intel_decompressed/geoipv6.mmdb
    RUN touch "./output/intel_decompressed/index.dsd"
    RUN touch "./output/intel_decompressed/base.dsdl"
    RUN touch "./output/intel_decompressed/intermediate.dsdl"
    RUN touch "./output/intel_decompressed/urgent.dsdl"

    # Save all artifacts to output folder
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/bin-index.json" AS LOCAL "${outputDir}/binary/bin-index.json"
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/all/*" AS LOCAL "${outputDir}/binary/all/"
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/linux_amd64/*" AS LOCAL "${outputDir}/binary/linux_amd64/"
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/windows_amd64/*" AS LOCAL "${outputDir}/binary/windows_amd64/"
    SAVE ARTIFACT --if-exists --keep-ts "output/intel/*" AS LOCAL "${outputDir}/intel/"
    SAVE ARTIFACT --if-exists --keep-ts "output/intel_decompressed/*" AS LOCAL "${outputDir}/intel_decompressed/"

    # Save all artifacts to the container output folder so other containers can access it.
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/bin-index.json" "output/binary/bin-index.json"
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/all/*" "output/binary/all/"
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/linux_amd64/*" "output/binary/linux_amd64/"
    SAVE ARTIFACT --if-exists --keep-ts "output/binary/windows_amd64/*" "output/binary/windows_amd64/"
    SAVE ARTIFACT --if-exists --keep-ts "output/intel/*" "output/intel/"
    SAVE ARTIFACT --if-exists --keep-ts "output/intel_decompressed/*" "output/intel_decompressed/"

installer-linux:
    FROM +rust-base
    # ARG --required target
    ARG target="x86_64-unknown-linux-gnu"

    WORKDIR /app/tauri
    COPY --keep-ts ./desktop/tauri/ .
    COPY assets/data ./../../assets/data
    COPY packaging ./../../packaging

    WORKDIR /app/tauri/src-tauri

    SAVE IMAGE --cache-hint

    DO +RUST_TO_GO_ARCH_STRING --rustTarget="${target}"

    # Build and copy the binaries
    RUN mkdir -p target/${target}/release
    COPY (+release-prep/output/binary/linux_amd64/portmaster) ./target/${target}/release/portmaster

    RUN mkdir -p binary
    COPY (+release-prep/output/binary/bin-index.json) ./binary/bin-index.json
    COPY (+release-prep/output/binary/linux_amd64/portmaster-core) ./binary/portmaster-core
    COPY (+release-prep/output/binary/all/portmaster.zip) ./binary/portmaster.zip
    COPY (+release-prep/output/binary/all/assets.zip) ./binary/assets.zip

    # Download the intel data
    RUN mkdir -p intel
    COPY (+release-prep/output/intel_decompressed/*) ./intel/

    # build the installers
    RUN cargo tauri bundle --ci --target="${target}"

    # Installers
    SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/bundle/deb/*.deb" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/"
    SAVE ARTIFACT --if-exists --keep-ts "target/${target}/release/bundle/rpm/*.rpm" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/"

kext-build:
    FROM ${rust_builder_image}

    # Install architecture target
    DO rust+INIT --keep_fingerprints=true

    # Build kext
    WORKDIR /app/kext
    # --keep-ts is necessary to ensure that the timestamps of the source files
    # are preserved such that Rust's incremental compilation works correctly.
    COPY --keep-ts ./windows_kext/ .

    # Add target architecture
    RUN rustup target add x86_64-pc-windows-msvc
    
    # Build using special earthly lib
    WORKDIR /app/kext/release
    DO rust+CARGO --args="run"

    SAVE ARTIFACT --keep-ts "portmaster-kext-release-bundle.zip" AS LOCAL "${outputDir}/windows_amd64/portmaster-kext-release-bundle.zip"


# Takes GOOS, GOARCH and optionally GOARM and creates a string representation for file-names.
# in the form of ${GOOS}_{GOARCH} if GOARM is empty, otherwise ${GOOS}_${GOARCH}v${GOARM}.
# Thats the same format as expected and served by our update server.
#
# The result is available as GO_ARCH_STRING environment variable in the build context.
GO_ARCH_STRING:
    FUNCTION
    ARG --required goos
    ARG --required goarch
    ARG goarm

    LET result = "${goos}_${goarch}"
    IF [ "${goarm}" != "" ]
        SET result = "${goos}_${goarch}v${goarm}"
    END

    ENV GO_ARCH_STRING="${result}"

# Takes a rust target (--rustTarget) and extracts architecture and OS and arm version
# and finally calls GO_ARCH_STRING.
#
# The result is available as GO_ARCH_STRING environment variable in the build context.
# It also exports GOOS, GOARCH and GOARM environment variables.
RUST_TO_GO_ARCH_STRING:
    FUNCTION
    ARG --required rustTarget

    LET goos=""
    IF [ -z "${rustTarget##*linux*}" ]
        SET goos="linux"
    ELSE IF [ -z "${rustTarget##*windows*}" ]
        SET goos="windows"
    ELSE IF [ -z "${rustTarget##*darwin*}" ]
        SET goos="darwin"
    ELSE
        RUN echo "GOOS not detected"; \
            exit 1;
    END

    LET goarch=""
    LET goarm=""
    IF [ -z "${rustTarget##*x86_64*}" ]
        SET goarch="amd64"
    ELSE IF [ -z "${rustTarget##*arm*}" ]
        SET goarch="arm"
        SET goarm="6"

        IF [ -z "${rustTarget##*v7*}" ]
            SET goarm="7"
        END
    ELSE IF [ -z "${rustTarget##*aarch64*}" ]
        SET goarch="arm64"
    ELSE
        RUN echo "GOARCH not detected"; \
            exit 1;
    END

    ENV GOOS="${goos}"
    ENV GOARCH="${goarch}"
    ENV GOARM="${goarm}"

    DO +GO_ARCH_STRING --goos="${goos}" --goarch="${goarch}" --goarm="${goarm}"

# Takes an architecture or GOOS string and sets the BINEXT env var.
BIN_EXT:
    FUNCTION
    ARG --required arch

    LET binext=""
    IF [ -z "${arch##*windows*}" ]
        SET binext=".exe"
    END
    ENV BINEXT="${goos}"
