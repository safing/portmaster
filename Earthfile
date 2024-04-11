VERSION --arg-scope-and-set --global-cache 0.8

ARG --global go_version = 1.22
ARG --global node_version = 18
ARG --global rust_version = 1.76

ARG --global go_builder_image = "golang:${go_version}-alpine"
ARG --global node_builder_image = "node:${node_version}"
ARG --global rust_builder_image = "rust:${rust_version}-bookworm"
ARG --global work_image = "alpine"

ARG --global outputDir = "./dist"

# The list of rust targets we support. They will be automatically converted
# to GOOS, GOARCH and GOARM when building go binaries. See the +RUST_TO_GO_ARCH_STRING
# helper method at the bottom of the file.


ARG --global architectures = "x86_64-unknown-linux-gnu" \
                             "aarch64-unknown-linux-gnu" \
                             "x86_64-pc-windows-gnu"

# Compile errors here:
#                             "armv7-unknown-linux-gnueabihf" \
#                             "arm-unknown-linux-gnueabi" \

# Import the earthly rust lib since it already provides some useful
# build-targets and methods to initialize the rust toolchain.
IMPORT github.com/earthly/lib/rust:3.0.2 AS rust

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

    # Copy the git folder and extract version information
    COPY .git ./.git

    LET version = $(git tag --points-at)
    IF [ "${version}" = "" ]
        SET version = "$(git describe --tags --abbrev=0)_dev_build"
    END
    IF [ "${version}" = "" ]
        SET version = "dev_build"
    END
    ENV VERSION="${version}"

    LET source = $( ( git remote -v | cut -f2 | cut -d" " -f1 | head -n 1 ) || echo "unknown" )
    ENV SOURCE="${source}"

    LET build_time = $(date -u "+%Y-%m-%dT%H:%M:%SZ" || echo "unknown")
    ENV BUILD_TIME = "${build_time}"

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

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

    CACHE --sharing shared "$GOCACHE"
    CACHE --sharing shared "$GOMODCACHE"

    RUN mkdir /tmp/build
    ENV CGO_ENABLED = "0"

    IF [ "${CMDS}" = "" ]
        LET CMDS=$(ls -1 "./cmds/")
    END

    # Build all go binaries from the specified in CMDS
    FOR bin IN $CMDS
        RUN --no-cache go build  -ldflags="-X github.com/safing/portbase/info.version=${VERSION} -X github.com/safing/portbase/info.buildSource=${SOURCE} -X github.com/safing/portbase/info.buildTime=${BUILD_TIME}" -o "/tmp/build/" ./cmds/${bin}
    END

    DO +GO_ARCH_STRING --goos="${GOOS}" --goarch="${GOARCH}" --goarm="${GOARM}"

    FOR bin IN $(ls -1 "/tmp/build/")
        SAVE ARTIFACT "/tmp/build/${bin}" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/${bin}"
    END

    SAVE ARTIFACT "/tmp/build/" ./output

# Test one or more go packages.
# Test are always run as -short, as "long" tests require a full desktop system.
# Run `earthly +test-go` to test all packages
# Run `earthly +test-go --PKG="service/firewall"` to only test a specific package.
# Run `earthly +test-go --TESTFLAGS="-args arg1"` to add custom flags to go test (-args in this case)
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
        RUN --no-cache go test -cover -short ${pkg} ${TESTFLAGS}
    END

test-go-all-platforms:
    FROM ${work_image}

    FOR arch IN ${architectures}
        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${arch}"
        BUILD +test-go --GOARCH="${GOARCH}" --GOOS="${GOOS}" --GOARM="${GOARM}"
    END

# Builds portmaster-start, portmaster-core, hub and notifier for all supported platforms
build-go-release:
    FROM ${work_image}

    FOR arch IN ${architectures}
        DO +RUST_TO_GO_ARCH_STRING --rustTarget="${arch}"

        IF [ -z GOARCH ]
            RUN echo "Failed to extract GOARCH for ${arch}"; exit 1
        END

        IF [ -z GOOS ]
            RUN echo "Failed to extract GOOS for ${arch}"; exit 1
        END

        BUILD +build-go --GOARCH="${GOARCH}" --GOOS="${GOOS}" --GOARM="${GOARM}"
    END

# Builds all binaries from the cmds/ folder for linux/windows AMD64
# Most utility binaries are never needed on other platforms.
build-utils:
    BUILD +build-go --CMDS="" --GOARCH=amd64 --GOOS=linux
    BUILD +build-go --CMDS="" --GOARCH=amd64 --GOOS=windows

# Prepares the angular project by installing dependencies
angular-deps:
    FROM ${node_builder_image}
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
    SAVE ARTIFACT "${dist}" "./output/${project}"
    
    # Save portmaster UI as local artifact.
    IF [ "${project}" = "portmaster" ]
        SAVE ARTIFACT "./${project}.zip" AS LOCAL ${outputDir}/all/${project}-ui.zip
    END

# Build the angular projects (portmaster-UI and tauri-builtin) in dev mode
angular-dev:
    BUILD +angular-project --project=portmaster --dist=./dist --configuration=development --baseHref=/ui/modules/portmaster/
    BUILD +angular-project --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=development --baseHref=/

# Build the angular projects (portmaster-UI and tauri-builtin) in production mode
angular-release:
    BUILD +angular-project --project=portmaster --dist=./dist --configuration=production --baseHref=/ui/modules/portmaster/
    BUILD +angular-project --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=production --baseHref=/

# A base target for rust to prepare the build container
rust-base:
    FROM ${rust_builder_image}

    RUN dpkg --add-architecture armhf
    RUN dpkg --add-architecture arm64

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
        linux-libc-dev-arm64-cross \
        linux-libc-dev-armel-cross \
        linux-libc-dev-armhf-cross \
        build-essential \
        curl \
        wget \
        file \
        libsoup-3.0-dev \
        libwebkit2gtk-4.1-dev 

    # Install library dependencies for all supported architectures
    # required for succesfully linking.
    FOR arch IN amd64 arm64 armhf
        RUN apt-get install --no-install-recommends -qq \
            libsoup-3.0-0:${arch} \
            libwebkit2gtk-4.1-0:${arch} \
            libssl3:${arch} \
            libayatana-appindicator3-1:${arch} \
            librsvg2-bin:${arch} \
            libgtk-3-0:${arch} \
            libjavascriptcoregtk-4.1-0:${arch}  \
            libssl-dev:${arch} \
            libayatana-appindicator3-dev:${arch} \
            librsvg2-dev:${arch} \
            libgtk-3-dev:${arch} \
            libjavascriptcoregtk-4.1-dev:${arch}  
   END

   # Note(ppacher): I've no idea why we need to explicitly create those symlinks:
   # Some how all the other libs work but libsoup and libwebkit2gtk do not create the link file
   RUN cd /usr/lib/aarch64-linux-gnu && \
        ln -s libwebkit2gtk-4.1.so.0 libwebkit2gtk-4.1.so && \
        ln -s libsoup-3.0.so.0 libsoup-3.0.so

   RUN cd /usr/lib/arm-linux-gnueabihf && \
        ln -s libwebkit2gtk-4.1.so.0 libwebkit2gtk-4.1.so && \
        ln -s libsoup-3.0.so.0 libsoup-3.0.so

    # For what ever reason trying to install the gcc compilers together with the above
    # command makes apt fail due to conflicts with gcc-multilib. Installing in a separate
    # step seems to work ...
    RUN apt-get install --no-install-recommends -qq \
        g++-mingw-w64-x86-64 \
        gcc-aarch64-linux-gnu \
        gcc-arm-none-eabi \
        gcc-arm-linux-gnueabi \
        gcc-arm-linux-gnueabihf \
        libc6-dev-arm64-cross \
        libc6-dev-armel-cross \
        libc6-dev-armhf-cross \
        libc6-dev-amd64-cross

    # Add some required rustup components
    RUN rustup component add clippy
    RUN rustup component add rustfmt

    # Install architecture targets
    FOR arch IN ${architectures}
        RUN rustup target add ${arch}
    END

    DO rust+INIT --keep_fingerprints=true

    # For now we need tauri-cli 1.5 for bulding
    DO rust+CARGO --args="install tauri-cli --version ^1.5.11"

    # Required for cross compilation to work.
    ENV PKG_CONFIG_ALLOW_CROSS=1

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

tauri-src:
    FROM +rust-base

    WORKDIR /app/tauri

    # --keep-ts is necessary to ensure that the timestamps of the source files
    # are preserved such that Rust's incremental compilation works correctly.
    COPY --keep-ts ./desktop/tauri/ .
    COPY assets/data ./assets
    COPY packaging/linux ./../../packaging/linux
    COPY (+angular-project/output/tauri-builtin --project=tauri-builtin --dist=./dist/tauri-builtin --configuration=production --baseHref="/") ./../angular/dist/tauri-builtin

    WORKDIR /app/tauri/src-tauri

    # Explicitly cache here.
    SAVE IMAGE --cache-hint

build-tauri:
    FROM +tauri-src

    ARG --required target
    ARG output=".*/release/(([^\./]+|([^\./]+\.(dll|exe)))|bundle/.*\.(deb|msi|AppImage))"
    ARG bundle="none"


    # if we want tauri to create the installer bundles we also need to provide all external binaries
    # we need to do some magic here because tauri expects the binaries to include the rust target tripple.
    # We already knwo that triple because it's a required argument. From that triple, we use +RUST_TO_GO_ARCH_STRING
    # function from below to parse the triple and guess wich GOOS and GOARCH we need.
    RUN mkdir /tmp/gobuild
    RUN mkdir ./binaries

    DO +RUST_TO_GO_ARCH_STRING --rustTarget="${target}"
    RUN echo "GOOS=${GOOS} GOARCH=${GOARCH} GOARM=${GOARM} GO_ARCH_STRING=${GO_ARCH_STRING}"

    # Our tauri app has externalBins configured so tauri will try to embed them when it finished compiling
    # the app. Make sure we copy portmaster-start and portmaster-core in all architectures supported.
    # See documentation for externalBins for more information on how tauri searches for the binaries.

    COPY (+build-go/output --GOOS="${GOOS}" --CMDS="portmaster-start portmaster-core" --GOARCH="${GOARCH}" --GOARM="${GOARM}") /tmp/gobuild

    # Place them in the correct folder with the rust target tripple attached.
    FOR bin IN $(ls /tmp/gobuild)
        # ${bin$.*} does not work in SET commands unfortunately so we use a shell
        # snippet here:
        RUN set -e ; \
            dest="./binaries/${bin}-${target}" ; \
            if [ -z "${bin##*.exe}" ]; then \
                dest="./binaries/${bin%.*}-${target}.exe" ; \
            fi ; \
            cp "/tmp/gobuild/${bin}" "${dest}" ;
    END

    # Just for debugging ...
    RUN ls -R ./binaries

    # The following is exected to work but doesn't. for whatever reason cargo-sweep errors out on the windows-toolchain.
    #
    #   DO rust+CARGO --args="tauri build --bundles none --ci --target=${target}" --output="release/[^/\.]+"
    #
    # For, now, we just directly mount the rust target cache and call cargo ourself.

    DO rust+SET_CACHE_MOUNTS_ENV
    RUN --mount=$EARTHLY_RUST_TARGET_CACHE cargo tauri build --bundles "${bundle}" --ci --target="${target}"
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

    # RUN echo output: $(ls "target/${target}/release")
    LET outbin="error"
    FOR bin IN "portmaster Portmaster.exe WebView2Loader.dll"
        # Modify output binary.
        SET outbin="${bin}"
        IF [ "${bin}" = "portmaster" ]
            SET outbin="portmaster-app"
        ELSE IF [ "${bin}" = "Portmaster.exe" ]
            SET outbin="portmaster-app.exe"
        END
        # Save output binary as local artifact.
        IF [ -f "target/${target}/release/${bin}" ]
            SAVE ARTIFACT "target/${target}/release/${bin}" AS LOCAL "${outputDir}/${GO_ARCH_STRING}/${outbin}"
        END
    END

tauri-release:
    FROM ${work_image}

    ARG bundle="none"

    FOR arch IN ${architectures}
        BUILD +build-tauri --target="${arch}" --bundle="${bundle}"
    END

build-all:
    BUILD +build-go-release
    BUILD +angular-release
    BUILD +tauri-release

release:
    LOCALLY

    IF ! git diff --quiet
        RUN echo -e "\033[1;31m Refusing to release a dirty git repository. Please commit your local changes first! \033[0m" ; exit 1
    END

    BUILD +build-all


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

# Takes an architecture or GOOS string and sets the BINEXT env var.
BIN_EXT:
    FUNCTION
    ARG --required arch

    LET binext=""
    IF [ -z "${arch##*windows*}" ]
        SET binext=".exe"
    END
    ENV BINEXT="${goos}"
