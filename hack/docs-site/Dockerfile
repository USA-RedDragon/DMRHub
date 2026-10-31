# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

ARG DMRGO_REF=main

FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS dmrgo-clone
ARG DMRGO_REF
RUN apk add --no-cache git
RUN git clone --quiet --filter=blob:none --no-checkout https://github.com/USA-RedDragon/dmrgo.git /dmrgo \
 && git -C /dmrgo checkout --quiet --detach "$DMRGO_REF"

FROM scratch AS dmrgo
COPY --from=dmrgo-clone /dmrgo /

FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS site
RUN apk add --no-cache git pandoc-cli python3 zstd
COPY --from=dmrgo / /src/dmrgo
COPY . /src/hub
RUN git config --global --add safe.directory '*' \
 && python3 /src/hub/hack/docs-site/build.py --hub /src/hub --dmrgo /src/dmrgo --out /site \
 && find /site -type f \( -name '*.html' -o -name '*.css' -o -name '*.js' -o -name '*.svg' -o -name '*.xml' \) -exec zstd -q -19 -k {} +

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS server
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY hack/docs-site/server ./hack/docs-site/server
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /docs-server ./hack/docs-site/server

FROM scratch
COPY --from=server /docs-server /docs-server
COPY --from=site /site /site
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/docs-server", "-root", "/site", "-addr", ":8080"]
