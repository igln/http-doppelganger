FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o gitlab-proxy ./cmd/proxy

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/gitlab-proxy .

RUN mkdir -p /var/www/certbot /etc/letsencrypt

EXPOSE 80 443 22

ENTRYPOINT ["./gitlab-proxy"]
CMD ["-config", "/app/config.yaml"]
