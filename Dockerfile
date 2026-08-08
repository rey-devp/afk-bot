# Stage 1: Build the Go binary
FROM golang:1.25-bookworm AS builder

WORKDIR /app

# Install all build dependencies for CGO + libdave
RUN apt-get update && apt-get install -y \
    gcc g++ cmake pkg-config git curl unzip bash build-essential ninja-build

# Clone godave and run the libdave install script
RUN git clone https://github.com/disgoorg/godave /tmp/godave && \
    chmod +x /tmp/godave/scripts/libdave_install.sh && \
    NON_INTERACTIVE=1 /bin/bash /tmp/godave/scripts/libdave_install.sh

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
FROM debian:bookworm-slim

# Install CA certificates and standard C++ runtime
RUN apt-get update && apt-get install -y ca-certificates libstdc++6 && rm -rf /var/lib/apt/lists/*

WORKDIR /root/

# Copy the compiled binary
COPY --from=builder /app/bot-afk .

# Copy the libdave shared library from the builder
COPY --from=builder /root/.local/lib/libdave.so /usr/local/lib/

# Update linker cache and set LD_LIBRARY_PATH
ENV LD_LIBRARY_PATH="/usr/local/lib"
RUN ldconfig

EXPOSE 8080

CMD ["./bot-afk"]
