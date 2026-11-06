# syntax=docker/dockerfile:1

ARG DMRGO_REF=main
ARG SOURCES_RUN
ARG PREVIOUS_IMAGE=ghcr.io/usa-reddragon/dmrhub-docs:main

FROM --platform=$BUILDPLATFORM ${PREVIOUS_IMAGE} AS previous

FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS dmrgo-clone
ARG DMRGO_REF
ARG SOURCES_RUN
RUN apk add --no-cache git
RUN git clone --quiet --filter=blob:none --no-checkout https://github.com/USA-RedDragon/dmrgo.git /dmrgo \
 && git -C /dmrgo checkout --quiet --detach "$DMRGO_REF"

FROM scratch AS dmrgo
COPY --from=dmrgo-clone /dmrgo /

FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS hubgit-clone
ARG SOURCES_RUN
RUN apk add --no-cache git
RUN git clone --quiet --bare --filter=blob:none https://github.com/USA-RedDragon/DMRHub.git /hub.git

FROM scratch AS hubgit
COPY --from=hubgit-clone /hub.git /

FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS site
RUN apk add --no-cache git pandoc-cli python3 zstd
COPY --from=dmrgo / /src/dmrgo
COPY --from=hubgit / /src/hub.git
COPY . /src/hub
RUN git config --global --add safe.directory '*' \
 && python3 /src/hub/hack/docs-site/build.py --hub /src/hub --hub-repo /src/hub.git --dmrgo /src/dmrgo --out /site \
 && find /site -type f \( -name '*.html' -o -name '*.css' -o -name '*.js' -o -name '*.svg' -o -name '*.xml' -o -name '*.json' \) -exec zstd -q -19 -k {} +
ARG CARRY_FIRST_BUILD
RUN --mount=type=bind,from=previous,target=/previous \
    sh /src/hub/hack/carry/carry-assets.sh \
      /previous/site/.carry-builds /site/.carry-builds \
      /previous/site/docs/_shell /site/docs/_shell \
      /previous/site/docs/_assets /site/docs/_assets

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS server
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
