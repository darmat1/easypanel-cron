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

# Build a statically-linked, optimized binary.
# -ldflags="-w -s" strips debug information to reduce binary size.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /runner main.go


# --- Stage 2: Final Image (Runner) ---
# Use a minimal Alpine image which is small and has a package manager.
FROM alpine:3.18

# Install ca-certificates for HTTPS support (good practice) and wget for the healthcheck.
RUN apk --no-cache add ca-certificates wget

# Set the working directory.
WORKDIR /

# Copy the compiled binary from the builder stage.
COPY --from=builder /runner /runner

# The HEALTHCHECK remains the same, but now it will work because wget is installed.
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -qO- http://localhost:8081/healthz || exit 1

# Specify the command to run when the container starts.
ENTRYPOINT ["/runner"]