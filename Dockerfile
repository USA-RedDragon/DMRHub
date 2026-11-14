FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/


USER 65534:65534
WORKDIR /app

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/DMRHub .
# The plugins that come with DMRHub. Run one with --entrypoint, such as
# /app/dmrhub-parrot.
COPY $TARGETPLATFORM/dmrhub-parrot $TARGETPLATFORM/dmrhub-locator ./

ENTRYPOINT ["/app/DMRHub"]
