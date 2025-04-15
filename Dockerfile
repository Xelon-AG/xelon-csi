# syntax=docker/dockerfile:1
FROM golang:1.24 AS builder

ARG GIT_COMMIT
ARG GIT_TREE_STATE
ARG SOURCE_DATE_EPOCH
ARG VERSION

ENV CGO_ENABLED=0

# copy manifest files only to cache layer with dependencies
WORKDIR /src/app/
COPY go.mod go.sum /src/app/
RUN go mod download
# copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# build
RUN go build -trimpath \
    -ldflags="-s -w \
    -X github.com/Xelon-AG/xelon-csi/internal/driver.gitCommit=${GIT_COMMIT:-none} \
    -X github.com/Xelon-AG/xelon-csi/internal/driver.gitTreeState=${GIT_TREE_STATE:-none} \
    -X github.com/Xelon-AG/xelon-csi/internal/driver.sourceDateEpoch=${SOURCE_DATE_EPOCH:-0} \
    -X github.com/Xelon-AG/xelon-csi/internal/driver.version=${VERSION:-local}" \
    -o xelon-csi cmd/xelon-csi/main.go



FROM alpine:3.21 AS production

ARG VERSION

LABEL org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.ref.name="xelon-csi" \
      org.opencontainers.image.source="https://github.com/Xelon-AG/xelon-csi" \
      org.opencontainers.image.vendor="Xelon AG" \
      org.opencontainers.image.version="${VERSION:-local}"

RUN <<EOF
    set -ex
    apk add --no-cache ca-certificates
    apk add --no-cache blkid
    apk add --no-cache e2fsprogs
    apk add --no-cache e2fsprogs-extra
    apk add --no-cache findmnt
    apk add --no-cache parted
    apk add --no-cache xfsprogs
    rm -rf /var/cache/apk/*
EOF

COPY --from=builder --chmod=755 /src/app/xelon-csi /xelon-csi

ENTRYPOINT ["/xelon-csi"]
