FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest@sha256:71e9b7bae160ba167fa17414401b94cbcdaf3c32feeab9298bd794a2a0614c35 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/


USER 65534:65534
WORKDIR /app

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/DMRHub .

ENTRYPOINT ["/app/DMRHub"]
