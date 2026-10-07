FROM golang:1.27.1-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/shortscale-api \
    ./cmd/api

FROM scratch

COPY --from=builder /out/shortscale-api /shortscale-api

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/shortscale-api"]
