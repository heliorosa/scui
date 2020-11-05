FROM golang:1.15.3-alpine3.12 AS build

RUN mkdir -p /src/scui && \
    cd /src/scui

COPY . /src/scui

RUN apk add gcc libc-dev linux-headers && \
    cd /src/scui && \
    go build -v ./cmd/scui && \
    go build -v ./cmd/scdeploy

FROM alpine:3.12.1

COPY --from=build /src/scui/scui /src/scui/scdeploy /usr/local/bin/
