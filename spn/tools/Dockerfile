FROM alpine as builder

# Ensure ca-certficates are up to date
# RUN update-ca-certificates

# Download and verify spn-hub binary.
RUN mkdir /init
RUN wget https://updates.safing.io/latest/linux_amd64/hub/spn-hub -O /init/spn-hub
COPY start-checksum.txt /init/start-checksum
RUN cd /init && sha256sum -c /init/start-checksum
RUN chmod 555 /init/spn-hub

# Use minimal image as base.
FROM alpine

# Copy the static executable.
COPY --from=builder /init/spn-hub /init/spn-hub

# Copy the init script
COPY container-init.sh /init.sh

# Run the hub.
ENTRYPOINT ["/init.sh"]
