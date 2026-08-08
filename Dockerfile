# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies for CGO (godave / DAVE E2EE protocol)
RUN apk add --no-cache gcc g++ musl-dev linux-headers

# Copy dependency files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code
COPY . .

# Build the application from cmd/bot
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o bot-afk ./cmd/bot

# Stage 2: Minimal production image
FROM alpine:latest

# Install CA certificates and C++ runtime required by godave
RUN apk --no-cache add ca-certificates libstdc++

WORKDIR /root/

# Copy the compiled binary
COPY --from=builder /app/bot-afk .

EXPOSE 8080

CMD ["./bot-afk"]
