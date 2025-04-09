FROM golang:1.20-alpine AS builder

WORKDIR /app

COPY go.mod ./
# COPY go.sum ./
# RUN go mod download

COPY . .
RUN go build -o node cmd/node/main.go

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/node /app/node

ENTRYPOINT ["/app/node"]
