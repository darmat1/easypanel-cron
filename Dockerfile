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
# Use a distroless image that contains common utilities like curl/wget.
# It's minimal, secure, and doesn't rely on Alpine's musl.
FROM gcr.io/distroless/cc-debian11

# Copy the compiled binary from the builder stage.
COPY --from=builder /runner /runner

# Add a HEALTHCHECK that queries our built-in HTTP health endpoint.
# This is more reliable than checking for a process ID.
# `wget` is used as it's available in this base image.
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -qO- http://localhost:8081/healthz || exit 1

# Specify the command to run when the container starts.
ENTRYPOINT ["/runner"]