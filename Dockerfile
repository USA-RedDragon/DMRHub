FROM golang:1.20-alpine AS builder

WORKDIR /DMRHub

RUN apk update && apk add --no-cache git make nodejs npm bash

COPY . .

ARG IS_CI=false

# If this is a CI build, we need to use build-ci instead of build
RUN if [ "$IS_CI" = "true" ]; then make build-ci; else make build; fi

FROM golang:alpine

RUN apk update && apk add --no-cache ca-certificates

COPY --from=builder /DMRHub/bin/DMRHub /DMRHub

ENTRYPOINT ["/DMRHub"]
