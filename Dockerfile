FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY DMRHub /

ENV DMRDatabaseDirectory=/dmrdb

ENTRYPOINT ["/DMRHub"]
