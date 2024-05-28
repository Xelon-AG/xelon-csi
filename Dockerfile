# syntax=docker/dockerfile:1
FROM alpine:3.19

ARG VERSION

LABEL org.opencontainers.image.ref.name="xelon-csi" \
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

COPY --chmod=755 xelon-csi /bin/xelon-csi

ENTRYPOINT ["/bin/xelon-csi"]

# syntax=docker/dockerfile:1
#FROM golang:1.22 AS builder
#
#ENV CGO_ENABLED=0
#
## copy manifest files only to cache layer with dependencies
#WORKDIR /src/app/
#COPY go.mod go.sum /src/app/
#RUN go mod download
## copy source code
#COPY cmd/ cmd/
#COPY internal/ internal/
#
## build
#RUN go build -o xelon-cloud-controller-manager -ldflags="-s -w" -trimpath cmd/xelon-cloud-controller-manager/main.go
#
#
#
#FROM alpine:3.19 AS production
#
#ARG VERSION
#
#LABEL org.opencontainers.image.ref.name="xelon-csi" \
#      org.opencontainers.image.source="https://github.com/Xelon-AG/xelon-csi" \
#      org.opencontainers.image.vendor="Xelon AG" \
#      org.opencontainers.image.version="${VERSION:-local}"
#
#WORKDIR /
#USER 65532:65532
#
#COPY --from=builder --chmod=755 /src/app/xelon-cloud-controller-manager /xelon-cloud-controller-manager
#
#ENTRYPOINT ["/xelon-cloud-controller-manager"]
