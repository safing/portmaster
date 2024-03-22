VERSION --arg-scope-and-set --global-cache 0.8

ARG --global go_version = 1.21
ARG --global distro = alpine3.18
ARG --global node_version = 18
ARG --global outputDir = "./dist"

# The list of rust targets we support. They will be automatically converted
# to GOOS, GOARCH and GOARM when building go binaries. See the +RUST_TO_GO_ARCH_STRING
# helper method at the bottom of the file.
ARG --global architectures = "x86_64-unknown-linux-gnu" \
                             "aarch64-unknown-linux-gnu" \
                             "armv7-unknown-linux-gnueabihf" \
                             "arm-unknown-linux-gnueabi" \
                             "x86_64-pc-windows-gnu"

# Import the earthly rust lib since it already provides some useful
# build-targets and methods to initialize the rust toolchain.
IMPORT github.com/earthly/lib/rust:3.0.2 AS rust

go-deps:
    FROM golang:${go_version}-${distro}
    WORKDIR /go-workdir

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

go-base:
    FROM +go-deps

    # Only copy go-code related files to improve caching.
    # (i.e. do not rebuild go if only the angular app changed)
    COPY cmds ./cmds
    COPY runtime ./runtime
    COPY service ./service
    COPY spn ./spn

    # The cmds/notifier embeds some icons but go:embed is not allowed
    # to leave the package directory so there's a small go-package in
    # assets. Once we drop the notify in favor of the tauri replacement
    # we can remove the following line and also remove all go-code from
    # ./assets
    COPY assets ./assets

# updates all go dependencies and runs go mod tidy, saving go.mod and go.sum locally.
update-go-deps:
    FROM +go-base

    RUN go get -u ./..
    RUN go mod tidy
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT --if-exists go.sum AS LOCAL go.sum

# mod-tidy runs 'go mod tidy', saving go.mod and go.sum locally.
mod-tidy:
    FROM +go-base

    RUN go mod tidy
    SAVE ARTIFACT go.mod AS LOCAL go.mod
    SAVE ARTIFACT --if-exists go.sum AS LOCAL go.sum

# build-go runs 'go build ./cmds/...', saving artifacts locally.
# If --CMDS is not set, it defaults to building portmaster-start, portmaster-core and hub
build-go:
    FROM +go-base

    # Arguments for cross-compilation.
    ARG GOOS=linux
    ARG GOARCH=amd64
    ARG GOARM
    ARG CMDS=portmaster-start portmaster-core hub notifier

    # Get the current version
    DO +GET_VERSION

    CACHE --sharing shared "$GOCACHE"
    CACHE --sharing shared "$GOMODCACHE"

    RUN mkdir /tmp/build
    ENV CGO_ENABLED = "0"

    IF [ "${CMDS}" = "" ]
        LET CMDS=$(ls -1 "./cmds/")
    END

    # Build all go binaries from the specified in CMDS
    FOR bin IN $CMDS
        RUN go build  -o "/tmp/build/" ./cmds/${bin}
    END

    FOR bin IN $(ls -1 "/tmp/build/")
        DO +GO_ARCH_STRING --goos="${GOOS}" --goarch="${GOARCH}" --goarm="${GOARM}"

        SAVE ARTIFACT "/tmp/build/${bin}" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/${bin}"
    END

    SAVE ARTIFACT "/tmp/build/" ./output

# Test one or more go packages.
# Run `earthly +test-go` to test all packages
# Run `earthly +test-go --PKG="service/firewall"` to only test a specific package.
# Run `earthly +test-go --TESTFLAGS="-short"` to add custom flags to go test (-short in this case)
test-go:
    FROM +go-base

    ARG GOOS=linux
    ARG GOARCH=amd64
    ARG GOARM
    ARG TESTFLAGS
    ARG PKG="..."

    CACHE --sharing shared "$GOCACHE"
    CACHE --sharing shared "$GOMODCACHE"

    FOR pkg IN $(go list -e "./${PKG}")
        RUN go test -cover ${TESTFLAGS} ${pkg}
    END

test-go-all-platforms:
    LOCALLY 
    FOR arch IN ${architectures}
        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${arch}"
        BUILD +test-go --GOARCH="${GOARCH}" --GOOS="${GOOS}" --GOARM="${GOARM}"
    END

# Builds portmaster-start, portmaster-core, hub and notifier for all supported platforms
build-go-release:
    LOCALLY
    FOR arch IN ${architectures}
        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${arch}"
        BUILD +build-go --GOARCH="${GOARCH}" --GOOS="${GOOS}" --GOARM="${GOARM}"
    END

# Builds all binaries from the cmds/ folder for linux/windows AMD64
# Most utility binaries are never needed on other platforms.
build-utils:
    BUILD +build-go --CMDS="" --GOARCH=amd64 --GOOS=linux
    BUILD +build-go --CMDS="" --GOARCH=amd64 --GOOS=windows

# Prepares the angular project by installing dependencies
angular-deps:
    FROM node:${node_version}
    WORKDIR /app/ui

    RUN apt update && apt install zip

    COPY desktop/angular/package.json .
    COPY desktop/angular/package-lock.json .
    COPY assets/data ./assets

    RUN npm install

# Copies the UI folder into the working container
# and builds the shared libraries in the specified configuration (production or development)
angular-base:
    FROM +angular-deps
    ARG configuration="production"

    COPY desktop/angular/ .

    IF [ "${configuration}" = "production" ]
        RUN npm run build-libs
    ELSE
        RUN npm run build-libs:dev
    END

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

    RUN ./node_modules/.bin/ng build --configuration ${configuration} --base-href ${baseHref} "${project}"

    RUN zip -r "./${project}.zip" "${dist}"
    SAVE ARTIFACT "./${project}.zip" AS LOCAL ${outputDir}/${project}.zip
    SAVE ARTIFACT "./dist" AS LOCAL ${outputDir}/${project}

# Build the angular projects (portmaster-UI and tauri-builtin) in production mode
angular-release:
    BUILD +angular-project --project=portmaster --dist=./dist --configuration=production --baseHref=/ui/modules/portmaster

# Build the angular projects (portmaster-UI and tauri-builtin) in dev mode
angular-dev:
    BUILD +angular-project --project=portmaster --dist=./dist --configuration=development --baseHref=/ui/modules/portmaster

# A base target for rust to prepare the build container
rust-base:
    FROM rust:1.76-bookworm

    RUN apt-get update -qq
    RUN apt-get install --no-install-recommends -qq \
        autoconf \
        autotools-dev \
        libtool-bin \
        clang \
        cmake \
        bsdmainutils \
        g++-mingw-w64-x86-64 \
        gcc-aarch64-linux-gnu \
        gcc-arm-none-eabi \
        gcc-arm-linux-gnueabi \
        gcc-arm-linux-gnueabihf \
        libgtk-3-dev \
        libjavascriptcoregtk-4.1-dev \
        libsoup-3.0-dev \
        libwebkit2gtk-4.1-dev \
        build-essential \
        curl \
        wget \
        file \
        libssl-dev \
        libayatana-appindicator3-dev \
        librsvg2-dev

    # Add some required rustup components
    RUN rustup component add clippy
    RUN rustup component add rustfmt

    # Install toolchains and targets
    FOR arch IN ${architectures}
        RUN rustup target add ${arch}
    END

    DO rust+INIT --keep_fingerprints=true

    # For now we need tauri-cli 1.5 for bulding
    DO rust+CARGO --args="install tauri-cli --version ^1.5.11"

tauri-src:
    FROM +rust-base

    WORKDIR /app/tauri

    # --keep-ts is necessary to ensure that the timestamps of the source files
    # are preserved such that Rust's incremental compilation works correctly.
    COPY --keep-ts ./desktop/tauri/ .
    COPY assets/data ./assets
    COPY (+angular-project/dist/tauri-builtin --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=production --baseHref="/") ./../angular/dist/tauri-builtin

build-tauri:
    FROM +tauri-src

    ARG --required target
    ARG output="release/[^\./]+"
    ARG bundle="none"

    # if we want tauri to create the installer bundles we also need to provide all external binaries
    # we need to do some magic here because tauri expects the binaries to include the rust target tripple.
    # We already knwo that triple because it's a required argument. From that triple, we use +RUST_TO_GO_ARCH_STRING
    # function from below to parse the triple and guess wich GOOS and GOARCH we need.
    IF [ "${bundle}" != "none" ] 
        RUN mkdir /tmp/gobuild
        RUN mkdir ./binaries

        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${target}"
        RUN echo "GOOS=${GOOS} GOARCH=${GOARCH} GOARM=${GOARM} GO_ARCH_STRING=${GO_ARCH_STRING}"

        COPY (+build-go/output --GOOS="${GOOS}" --CMDS="portmaster-start portmaster-core" --GOARCH="${GOARCH}" --GOARM="${GOARM}") /tmp/gobuild

        LET dest=""
        FOR bin IN $(ls /tmp/gobuild)
            SET dest="./binaries/${bin}-${target}"

            IF [ -z "${bin##*.exe}" ]
                SET dest = "./binaries/${bin%.*}-${target}.exe"
            END

            RUN echo "Copying ${bin} to ${dest}"
            RUN cp "/tmp/gobuild/${bin}" "${dest}"
        END
    END

    # The following is exected to work but doesn't. for whatever reason cargo-sweep errors out on the windows-toolchain.
    #
    #   DO rust+CARGO --args="tauri build --bundles none --ci --target=${target}" --output="release/[^/\.]+"
    #
    # For, now, we just directly mount the rust target cache and call cargo ourself.

    DO rust+SET_CACHE_MOUNTS_ENV
    RUN --mount=$EARTHLY_RUST_TARGET_CACHE cargo tauri build --bundles "${bundle}" --ci --target="${target}"
    DO rust+COPY_OUTPUT --output="${output}"

    RUN ls target

tauri-release:
    LOCALLY

    ARG bundle="none"

    FOR arch IN ${architectures}
        BUILD +build-tauri --target="${arch}" --bundle="${bundle}"
    END

release:
    BUILD +build-go-release
    BUILD +angular-release


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
    ELSE
        SET goos="windows"
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

GET_VERSION:
    FUNCTION
    LOCALLY

    LET VERSION=$(git tag --points-at)
    IF [ -z "${VERSION}"]
        SET VERSION=$(git describe --tags --abbrev=0)§dev§build
    ELSE IF ! git diff --quite
        SET VERSION="${VERSION}§dev§build"
    END

    RUN echo "Version is ${VERSION}"
    ENV VERSION="${VERSION}"

test:
    LOCALLY

    DO +GET_VERSION