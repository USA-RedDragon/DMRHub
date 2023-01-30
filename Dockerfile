FROM golang:alpine AS builder

WORKDIR /dmrserver-in-a-box

RUN apk update && apk add --no-cache git make nodejs npm bash

COPY . .

RUN make build

FROM golang:alpine

RUN apk update && apk add --no-cache ca-certificates

COPY --from=builder /dmrserver-in-a-box/bin/dmrserver-in-a-box /dmrserver-in-a-box

ENTRYPOINT ["/dmrserver-in-a-box"]
