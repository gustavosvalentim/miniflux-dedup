# syntax=docker/dockerfile:1

FROM golang:1.25-alpine3.22 AS build

RUN apk add --no-cache ca-certificates tzdata
ENV GOTOOLCHAIN=auto

WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd/miniflux-dedup ./cmd/miniflux-dedup

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/miniflux-dedup \
    ./cmd/miniflux-dedup

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/miniflux-dedup /miniflux-dedup

USER 65532:65532
ENTRYPOINT ["/miniflux-dedup"]
CMD ["-dry-run"]
