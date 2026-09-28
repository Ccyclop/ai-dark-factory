# Stub image for self-testing the adversary's own scripts. NOT the service.
#
# Built only when a campaign script is run with --stub, to prove that the script's
# own plumbing (quoting, JSON reading, arithmetic, argument passing) works before
# the real routes exist. The image is never used for a real verdict.
FROM golang:1.25.14-alpine3.24 AS build
ENV CGO_ENABLED=0 GOFLAGS= GOTOOLCHAIN=local GOPROXY=off
WORKDIR /src
COPY main.go ./
RUN go build -trimpath -o /out/stub main.go

FROM alpine:3.24.1
COPY --from=build /out/stub /usr/local/bin/stub
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/stub"]
