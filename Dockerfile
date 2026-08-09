# Stage 1: Build the Go binary using Ubuntu 24.04 to satisfy GLIBC_2.38+ requirements of the prebuilt libdave
FROM ubuntu:24.04 AS builder

# Copy Go from the official golang image
COPY --from=golang:1.25-bookworm /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /app

# Install minimal build dependencies for CGO linking
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ pkg-config curl unzip ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Download prebuilt libdave from Discord's official releases
RUN LIBDAVE_URL="https://github.com/discord/libdave/releases/download/v1.1.0/cpp/libdave-Linux-X64-boringssl.zip" && \
    curl -fsSL "$LIBDAVE_URL" -o /tmp/libdave.zip && \
    mkdir -p /tmp/libdave && \
    unzip -o /tmp/libdave.zip -d /tmp/libdave && \
    mkdir -p /usr/local/lib /usr/local/include && \
    cp /tmp/libdave/lib/libdave.so /usr/local/lib/ && \
    cp /tmp/libdave/include/dave/dave.h /usr/local/include/dave.h && \
    ldconfig && \
    rm -rf /tmp/libdave /tmp/libdave.zip

# Create pkg-config file so Go/CGO can find libdave
RUN mkdir -p /usr/local/lib/pkgconfig && \
    echo 'prefix=/usr/local'            >  /usr/local/lib/pkgconfig/dave.pc && \
    echo 'libdir=${prefix}/lib'         >> /usr/local/lib/pkgconfig/dave.pc && \
    echo 'includedir=${prefix}/include' >> /usr/local/lib/pkgconfig/dave.pc && \
    echo ''                             >> /usr/local/lib/pkgconfig/dave.pc && \
    echo 'Name: dave'                   >> /usr/local/lib/pkgconfig/dave.pc && \
    echo 'Description: Discord DAVE E2EE library' >> /usr/local/lib/pkgconfig/dave.pc && \
    echo 'Version: 1.1.0'              >> /usr/local/lib/pkgconfig/dave.pc && \
    echo 'Libs: -L${libdir} -ldave'    >> /usr/local/lib/pkgconfig/dave.pc && \
    echo 'Cflags: -I${includedir}'     >> /usr/local/lib/pkgconfig/dave.pc

ENV PKG_CONFIG_PATH="/usr/local/lib/pkgconfig"
ENV LD_LIBRARY_PATH="/usr/local/lib"

# Copy dependency files first for better Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code
COPY . .

# Build the application with CGO enabled
RUN CGO_ENABLED=1 GOOS=linux go build -o bot-afk ./cmd/bot

# Ensure at least an empty cookies.txt exists so COPY doesn't fail
RUN touch /app/cookies.txt

# Stage 2: Minimal production image
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libstdc++6 ffmpeg curl python3 python3-pip unzip git \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 20.x
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs

# Install Deno
RUN curl -fsSL https://deno.land/install.sh | sh -s -- -y
ENV DENO_INSTALL="/root/.deno"
ENV PATH="${DENO_INSTALL}/bin:${PATH}"

# Install yt-dlp via pip (always gets the latest release/nightly)
RUN pip install --break-system-packages -U https://github.com/yt-dlp/yt-dlp/archive/master.zip

# Install PO Token Provider server
RUN git clone --single-branch --branch 1.3.1 https://github.com/Brainicism/bgutil-ytdlp-pot-provider.git /opt/bgutil-provider && \
    cd /opt/bgutil-provider/server && \
    npm ci && \
    npx tsc

# Install plugin Python untuk yt-dlp
RUN pip install --break-system-packages -U bgutil-ytdlp-pot-provider

WORKDIR /root/

# Copy the compiled binary and optional cookies files
COPY --from=builder /app/bot-afk /app/*cookies.tx[t] ./

# Copy libdave shared library
COPY --from=builder /usr/local/lib/libdave.so /usr/local/lib/
RUN ldconfig

ENV LD_LIBRARY_PATH="/usr/local/lib"

# Verify dependencies are installed correctly
RUN deno --version && yt-dlp --version && ffmpeg -version

EXPOSE 8080

COPY start.sh /root/start.sh
RUN chmod +x /root/start.sh
ENTRYPOINT ["/root/start.sh"]
