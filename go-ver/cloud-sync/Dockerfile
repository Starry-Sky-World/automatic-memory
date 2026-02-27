# syntax=docker/dockerfile:1.7

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/cloud-sync ./cmd/cloudsync

FROM alpine:3.21
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/cloud-sync /app/cloud-sync
COPY migrations /app/migrations
RUN mkdir -p /data

ENV PORT=8090
ENV CLOUD_SYNC_MIGRATIONS_DIR=/app/migrations
ENV CLOUD_SYNC_DATABASE_URL=file:/data/cloudsync.db
EXPOSE 8090

CMD ["/app/cloud-sync"]
