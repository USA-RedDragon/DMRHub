FROM golang:1.20-alpine AS builder

WORKDIR /DMRHub

RUN apk update && apk add --no-cache git make nodejs npm bash

COPY . .

RUN make build

FROM golang:alpine

RUN apk update && apk add --no-cache ca-certificates

COPY --from=builder /DMRHub/bin/DMRHub /DMRHub

ENTRYPOINT ["/DMRHub"]
