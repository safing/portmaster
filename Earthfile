VERSION --arg-scope-and-set 0.8

ARG --global go_version = 1.21
ARG --global distro = alpine3.18
ARG --global node_version = 18
ARG --global outputDir = "./dist"

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
    ARG CMDS=portmaster-start portmaster-core hub

    CACHE --sharing shared "$GOCACHE"
    CACHE --sharing shared "$GOMODCACHE"

    RUN mkdir /tmp/build
    ENV CGO_ENABLED = "0"

    IF [ "${CMDS}" = "" ]
        LET CMDS=$(ls -1 "./cmds/")
    END

    # Build all go binaries from the specified in CMDS
    FOR bin IN $CMDS
        RUN go build -o "/tmp/build/" ./cmds/${bin}
    END

    LET NAME = ""

    FOR bin IN $(ls -1 "/tmp/build/")
        SET NAME = "${outputDir}/${GOOS}_${GOARCH}/${bin}"
        IF [ "${GOARM}" != "" ]
            SET NAME = "${outputDir}/${GOOS}_${GOARCH}v${GOARM}/${bin}"
        END

        SAVE ARTIFACT "/tmp/build/${bin}" AS LOCAL "${NAME}"
    END

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
    # Linux platforms:
    BUILD +test-go --GOARCH=amd64 --GOOS=linux
    BUILD +test-go --GOARCH=arm64 --GOOS=linux
    BUILD +test-go --GOARCH=arm --GOOS=linux --GOARM=5
    BUILD +test-go --GOARCH=arm --GOOS=linux --GOARM=6
    BUILD +test-go --GOARCH=arm --GOOS=linux --GOARM=7

    # Windows platforms:
    BUILD +test-go --GOARCH=amd64 --GOOS=windows
    BUILD +test-go --GOARCH=arm64 --GOOS=windows

# Builds portmaster-start and portmaster-core for all supported platforms
build-go-release:
    # Linux platforms:
    BUILD +build-go --GOARCH=amd64 --GOOS=linux
    BUILD +build-go --GOARCH=arm64 --GOOS=linux
    BUILD +build-go --GOARCH=arm --GOOS=linux --GOARM=5
    BUILD +build-go --GOARCH=arm --GOOS=linux --GOARM=6
    BUILD +build-go --GOARCH=arm --GOOS=linux --GOARM=7

    # Windows platforms:
    BUILD +build-go --GOARCH=amd64 --GOOS=windows
    BUILD +build-go --GOARCH=arm64 --GOOS=windows

# Builds all binaries from the cmds/ folder for linux/windows AMD64
# Most utility binaries are never needed on other platforms.
build-utils:
    BUILD +build-go --CMDS="" --GOARCH=amd64 --GOOS=linux
    BUILD +build-go --CMDS="" --GOARCH=amd64 --GOOS=windows

# Prepares the angular project
angular-deps:
    FROM node:${node_version}
    WORKDIR /app/ui

    RUN apt update && apt install zip

    CACHE --sharing shared "/app/ui/node_modules"

    COPY desktop/angular/package.json .
    COPY desktop/angular/package-lock.json .
    RUN npm install


angular-base:
    FROM +angular-deps

    COPY desktop/angular/ .

# Build the Portmaster UI (angular) in release mode
angular-release:
    FROM +angular-base

    CACHE --sharing shared "/app/ui/node_modules"

    RUN npm run build
    RUN zip -r ./angular.zip ./dist
    SAVE ARTIFACT "./angular.zip" AS LOCAL ${outputDir}/angular.zip
    SAVE ARTIFACT "./dist" AS LOCAL ${outputDir}/angular


# Build the Portmaster UI (angular) in dev mode
angular-dev:
    FROM +angular-base

    CACHE --sharing shared "/app/ui/node_modules"

    RUN npm run build:dev
    SAVE ARTIFACT ./dist AS LOCAL ${outputDir}/angular


release:
    BUILD +build-go-release
    BUILD +angular-release
