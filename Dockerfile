# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o goroute ./cmd/goroute

# Runtime stage: scratch container for ultra-minimal size (~15MB total)
FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/goroute /usr/local/bin/goroute

EXPOSE 20128
VOLUME ["/root/.goroute"]

ENTRYPOINT ["goroute", "serve", "--port", "20128"]
