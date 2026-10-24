FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/


USER 65534:65534
WORKDIR /app

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/DMRHub .

ENTRYPOINT ["/app/DMRHub"]
