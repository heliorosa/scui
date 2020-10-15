FROM golang:1.15.3-alpine3.12 AS build

RUN mkdir -p /src/scui && \
    cd /src/scui

COPY . /src/scui

RUN cd /src/scui && \
    apk add gcc libc-dev linux-headers && \
    go build -v

FROM alpine:3.12.0

COPY --from=build /src/scui/scui /

ENTRYPOINT [ "/scui" ]
