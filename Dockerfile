FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest@sha256:3f4d6b32e26703c6082839234f43769d5046b3ac7c910033911d1e71d8cd630a /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/


USER 65534:65534
WORKDIR /app

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/DMRHub .
# The plugins that come with DMRHub. Run one with --entrypoint, such as
# /app/dmrhub-parrot.
COPY $TARGETPLATFORM/dmrhub-parrot $TARGETPLATFORM/dmrhub-locator ./

ENTRYPOINT ["/app/DMRHub"]
