FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/


USER 65534:65534
WORKDIR /app

COPY DMRHub .

ENTRYPOINT ["/app/DMRHub"]
