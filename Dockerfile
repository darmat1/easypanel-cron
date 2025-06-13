# --- Stage 1: Build the application (Builder) ---
# Use the official Go image for compilation.
FROM golang:1.21-alpine AS builder

# Set the working directory inside the container.
WORKDIR /app

# Copy dependency files and download them.
# This is cached to avoid re-downloading on every code change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code.
COPY . .

# Build the application.
# CGO_ENABLED=0 creates a static binary without C dependencies.
# -ldflags="-w -s" strips debug information to reduce binary size.
# -o /runner specifies the output path for the compiled file.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /runner main.go


# --- Stage 2: Final Image (Runner) ---
# Use the smallest possible base image to keep the service lightweight.
FROM alpine:latest

# Install the Docker CLI, which is required to execute `docker exec` commands.
RUN apk add --no-cache docker-cli

# Copy system certificates from the builder stage; these are needed for HTTPS requests.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy only the compiled binary file from the builder stage.
# Nothing extra, just the single executable.
COPY --from=builder /runner /runner

# Add a health check to let Docker know if the service is running correctly.
# It simply checks if the main process is alive.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD pgrep -x runner || exit 1

# Specify the command to run when the container starts.
ENTRYPOINT ["/runner"]