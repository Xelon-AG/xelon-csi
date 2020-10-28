FROM alpine:3.12

RUN apk add --no-cache ca-certificates && rm -rf /var/cache/apk/*
ADD build/xelon-csi /bin/

CMD ["/bin/xelon-csi"]
