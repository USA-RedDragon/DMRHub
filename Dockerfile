FROM golang:1.20-alpine AS builder

WORKDIR /DMRHub

ENV CGO_ENABLED=0

ARG IS_CI=false
ENV IS_CI=$IS_CI

RUN apk update && apk add --no-cache git make bash
RUN if [ "$IS_CI" = "false" ]; then apk add --no-cache nodejs npm; fi

COPY . .

# If this is a CI build, we need to use build-ci instead of build
RUN if [ "$IS_CI" = "true" ]; then make build-ci; else make build; fi

RUN if [ "$IS_CI" = "true" ]; then make test; fi

FROM golang:alpine

RUN apk update && apk add --no-cache ca-certificates

COPY --from=builder /DMRHub/bin/DMRHub /DMRHub

ENTRYPOINT ["/DMRHub"]
