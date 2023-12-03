FROM alpine:latest AS dir

RUN mkdir -p /dmrdb

FROM scratch

# this pulls directly from the upstream image, which already has ca-certificates:
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY DMRHub /

ENV DMR_DATABASE_DIRECTORY=/dmrdb
COPY --from=dir /dmrdb /dmrdb

ENTRYPOINT ["/DMRHub"]
