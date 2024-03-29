# Docker Image for Observation Hub

# Important:
# You need to build this from the repo root!
# Run: docker build -f cmds/observation-hub/Dockerfile -t safing/observation-hub:latest .
# Check With: docker run -ti --rm safing/observation-hub:latest --help

# golang 1.21 linux/amd64 on debian bookworm
# https://github.com/docker-library/golang/blob/master/1.21/bookworm/Dockerfile
FROM golang:1.21-bookworm as builder

# Ensure ca-certficates are up to date
RUN update-ca-certificates

# Install dependencies
WORKDIR $GOPATH/src/github.com/safing/portmaster/spn
COPY go.mod .
COPY go.sum .
ENV GO111MODULE=on
RUN go mod download
RUN go mod verify

# Copy source code
COPY . .

# Build the static binary
RUN cd cmds/observation-hub && \
CGO_ENABLED=0 ./build -o /go/bin/observation-hub

# Use static image
# https://github.com/GoogleContainerTools/distroless
FROM gcr.io/distroless/static-debian12

# Copy our static executable
COPY --from=builder --chmod=0755 /go/bin/observation-hub /go/bin/observation-hub

# Run the observation-hub binary.
ENTRYPOINT ["/go/bin/observation-hub"]
