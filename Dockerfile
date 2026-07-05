FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/bot .
COPY configs/ configs/
CMD ["./bot"]
