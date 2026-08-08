# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install ALL dependencies needed by libdave_install.sh and CGO build
RUN apk add --no-cache \
    gcc \
    g++ \
    musl-dev \
    linux-headers \
    cmake \
    pkgconf \
    git \
    bash \
    build-base \
    curl \
    unzip

# Clone godave and run the libdave install script with the correct version
RUN git clone https://github.com/disgoorg/godave /tmp/godave && \
    chmod +x /tmp/godave/scripts/libdave_install.sh && \
    NON_INTERACTIVE=1 /bin/bash /tmp/godave/scripts/libdave_install.sh v0.3.0

# Point pkg-config to where libdave_install.sh places the .pc file
ENV PKG_CONFIG_PATH="/root/.local/lib/pkgconfig"
ENV LD_LIBRARY_PATH="/root/.local/lib"

# Copy dependency files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o bot-afk ./cmd/bot

# Stage 2: Minimal production image
FROM alpine:latest

RUN apk --no-cache add ca-certificates libstdc++

WORKDIR /root/

# Copy the compiled binary
COPY --from=builder /app/bot-afk .

# Copy the libdave shared library from the builder
COPY --from=builder /root/.local/lib/ /usr/local/lib/

ENV LD_LIBRARY_PATH="/usr/local/lib"

EXPOSE 8080

CMD ["./bot-afk"]
