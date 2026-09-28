# Attack client for the adversary seat. Not a service build: C-RUN-2 and C-RUN-5
# do not apply to it. Stdlib only, so it builds with --network=none and runs in
# the service's network namespace under C-RUN-4.
FROM golang:1.25.14-alpine3.24 AS build
ENV CGO_ENABLED=0 GOFLAGS= GOTOOLCHAIN=local GOPROXY=off
WORKDIR /src
COPY main.go ./
RUN go build -trimpath -o /out/advtool main.go

FROM alpine:3.24.1
COPY --from=build /out/advtool /usr/local/bin/advtool
ENTRYPOINT ["/usr/local/bin/advtool"]
