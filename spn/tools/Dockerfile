FROM alpine as builder

# Ensure ca-certficates are up to date
# RUN update-ca-certificates

# Download and verify portmaster-start binary.
RUN mkdir /init
RUN wget https://updates.safing.io/linux_amd64/start/portmaster-start_v0-9-6 -O /init/portmaster-start 
COPY start-checksum.txt /init/start-checksum
RUN cd /init && sha256sum -c /init/start-checksum
RUN chmod 555 /init/portmaster-start

# Use minimal image as base.
FROM alpine

# Copy the static executable.
COPY --from=builder /init/portmaster-start /init/portmaster-start 

# Copy the init script
COPY container-init.sh /init.sh

# Run the hub.
ENTRYPOINT ["/init.sh"]
