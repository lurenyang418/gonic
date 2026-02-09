FROM --platform=$BUILDPLATFORM golang:1.25-alpine3.22 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT_HASH=unknown
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN  \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
    -ldflags="-X 'github.com/lurenyang418/gonic/pkg/version.Version=${VERSION}' -X 'github.com/lurenyang418/gonic/pkg/version.CommitHash=${COMMIT_HASH}'" \
    -o /out/ ./cmd/...

FROM alpine:3.23
LABEL org.opencontainers.image.source=https://github.com/lurenyang418/gonic
RUN apk add -U --no-cache \
    ca-certificates \
    tzdata \
    tini \
    shared-mime-info
COPY --from=builder /out/* /usr/local/bin/
VOLUME ["/data", "/music", "/podcasts"]
EXPOSE 80
ENV TZ=
ENV GONIC_DB_PATH=/data/gonic.db
ENV GONIC_LISTEN_ADDR=:80
ENV GONIC_MUSIC_PATH=/music
ENV GONIC_PODCAST_PATH=/podcasts
ENV GONIC_PLAYLISTS_PATH=/playlists
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["gonic"]
