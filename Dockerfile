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

CMD ["/bin/xelon-csi"]
