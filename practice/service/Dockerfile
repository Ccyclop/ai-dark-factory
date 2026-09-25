# Builds with no network: every Go dependency is in vendor/, the toolchain is
# never downloaded, and both base images are pinned by tag (pull them once).
FROM golang:1.25.14-alpine3.24 AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=vendor GOPROXY=off
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY *.go ./
RUN go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/practice .

FROM alpine:3.24.1
RUN adduser -D -H -u 10001 app && mkdir /data && chown app /data
COPY --from=build /out/practice /usr/local/bin/practice
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/practice"]
