# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install all build dependencies for CGO + libdave (DAVE E2EE protocol)
RUN apk add --no-cache \
    gcc \
    g++ \
    musl-dev \
    linux-headers \
    cmake \
    pkgconf \
    git \
    bash \
    build-base

# Clone godave and run the libdave install script
RUN git clone https://github.com/disgoorg/godave /tmp/godave && \
    chmod +x /tmp/godave/scripts/libdave_install.sh && \
    /bin/bash /tmp/godave/scripts/libdave_install.sh

# Make sure pkg-config can find the installed libdave
ENV PKG_CONFIG_PATH="/usr/local/lib/pkgconfig"

# Copy dependency files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code
COPY . .

# Build the application from cmd/bot
RUN CGO_ENABLED=1 GOOS=linux go build -o bot-afk ./cmd/bot

# Stage 2: Minimal production image
FROM alpine:latest

# Install CA certificates and C++ runtime required by libdave
RUN apk --no-cache add ca-certificates libstdc++

WORKDIR /root/

# Copy the compiled binary
COPY --from=builder /app/bot-afk .

# Copy the libdave shared library from the builder stage
COPY --from=builder /usr/local/lib/libdave.so* /usr/local/lib/

# Ensure the linker can find libdave at runtime
ENV LD_LIBRARY_PATH="/usr/local/lib"

EXPOSE 8080

CMD ["./bot-afk"]
