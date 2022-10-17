FROM golang:alpine as builder

ENV VERSION="v1.3.1"

WORKDIR $GOPATH/src/github.com/wwt
RUN \
    apk add --no-cache git && \
    git clone --branch $VERSION --depth 1 https://github.com/wwt/guac.git guac && \
    cd guac && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -mod=vendor -ldflags "-s -w -extldflags '-static'" -o guac ./cmd/guac

FROM scratch
COPY --from=builder /go/src/github.com/wwt/guac/guac /

EXPOSE 4567

ENTRYPOINT ["./guac"]
