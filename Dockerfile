# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Install build dependencies for CGO (godave / DAVE E2EE protocol)
RUN apk add --no-cache gcc g++ musl-dev linux-headers

# Copy the go.mod and go.sum files
COPY go.mod go.sum* ./

# Download all dependencies
RUN go mod download

# Copy the source code into the container
COPY . .

# Build the Go app with CGO_ENABLED=1
RUN CGO_ENABLED=1 GOOS=linux go build -o bot-afk .

# Stage 2: Create a minimal image for production
FROM alpine:latest

# Install CA certificates and standard C++ libraries required by godave
RUN apk --no-cache add ca-certificates libstdc++

WORKDIR /root/

# Copy the pre-built binary file from the previous stage
COPY --from=builder /app/bot-afk .

# Expose port 8080 to the outside world for the health check HTTP server
EXPOSE 8080

# Command to run the executable
CMD ["./bot-afk"]
