# Generic Cron Runner for Docker

A flexible, containerized, and multi-job cron runner designed for modern development environments like [Easypanel](https://easypanel.io/), Docker Swarm, or any Docker-based platform.

This tool allows you to centralize all your scheduled tasks into a single, easy-to-manage service. It moves the responsibility of cron scheduling away from individual application containers, providing a robust, observable, and unified solution.

[![Go Report Card](https://goreportcard.com/badge/github.com/darmat1/easypanel-cron)](https://goreportcard.com/report/github.com/darmat1/easypanel-cron)
[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

## Core Features

-   **Multi-Job Support**: Configure and run multiple, independent scheduled jobs from a single container.
-   **Two Job Types**:
    -   `http`: Ping a URL endpoint (e.g., a webhook or API).
    -   `shell`: Execute any shell command.
-   **Remote Command Execution**: Execute shell commands in other Docker containers on the same host using `docker exec`. Perfect for running `php artisan`, `rake`, `manage.py`, or database backups.
-   **Structured JSON Logging**: All output is in JSON format (`slog`), ready to be ingested by log management systems.
-   **Configuration via Environment Variables**: Easy to configure and deploy in any containerized environment.
-   **Graceful Shutdown**: Catches `SIGINT` and `SIGTERM` signals to ensure running jobs can finish before the container stops.
-   **Lightweight & Secure**: Built on a minimal `alpine` base image with a multi-stage Docker build.

## Table of Contents

-   [Getting Started](#getting-started)
    -   [Prerequisites](#prerequisites)
    -   [Building the Docker Image](#building-the-docker-image)
-   [How to Use](#how-to-use)
    -   [Running the Container](#running-the-container)
    -   [Configuration](#configuration)
-   [Configuration Examples](#configuration-examples)
    -   [Example 1: Single HTTP Job](#example-1-single-http-job)
    -   [Example 2: Local Shell Command Job](#example-2-local-shell-command-job)
    -   [Example 3: Remote Shell Command (in another container)](#example-3-remote-shell-command-in-another-container)
    -   [Example 4: Multiple Jobs Combined](#example-4-multiple-jobs-combined)
-   [Logging](#logging)
-   [Building from Source](#building-from-source)
-   [Contributing](#contributing)
-   [License](#license)

## Getting Started

### Prerequisites

-   [Docker](https://www.docker.com/) installed and running.
-   [Go](https://golang.org/) (v1.21+) if you wish to build from source.

### Building the Docker Image

Clone the repository and build the Docker image using the provided `Dockerfile`.

```bash
git clone https://github.com/darmat1/easypanel-cron.git
cd easypanel-cron
docker build -t darmat1/easypanel-cron .
```
You can also pull the pre-built image from Docker Hub (if you set up automated builds):
```bash
docker pull darmat1/easypanel-cron
```

## How to Use

### Running the Container

The application is configured entirely through environment variables. Here is a basic `docker run` command.

```bash
docker run -d \
  --name my-cron-runner \
  -e "CRON_SCHEDULE_1=*/5 * * * *" \
  -e "JOB_TYPE_1=http" \
  -e "CRON_TARGET_URL_1=https://example.com/api/health-check" \
  -e "CRON_SECRET_1=my-secret-token" \
  darmat1/easypanel-cron
```

**Important:** To execute shell commands in other containers, you must mount the Docker socket:

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ... # other environment variables
  darmat1/easypanel-cron
```

### Configuration

Jobs are defined using indexed environment variables (e.g., `_1`, `_2`, `_3`, etc.). The runner will load jobs sequentially until it cannot find a `CRON_SCHEDULE_i` for the next index.

#### General Job Variables

| Variable                | Description                                                                                               | Required? | Default       |
| ----------------------- | --------------------------------------------------------------------------------------------------------- | --------- | ------------- |
| `JOB_NAME_i`            | An optional, friendly name for the job, used in logs for clarity.                                         | No        | `job_#i`      |
| `CRON_SCHEDULE_i`       | The cron schedule string in standard format (`* * * * *`). **This variable must exist to define a job.** | **Yes**   | -             |
| `JOB_TYPE_i`            | The type of job to run. Can be `http` or `shell`.                                                         | No        | `http`        |

#### `http` Job Type Variables

These variables are required when `JOB_TYPE_i` is `http`.

| Variable                | Description                                               | Required? |
| ----------------------- | --------------------------------------------------------- | --------- |
| `CRON_TARGET_URL_i`     | The full URL to which a `GET` request will be sent.       | **Yes**   |
| `CRON_SECRET_i`         | A secret token sent in the `Authorization: Bearer` header. | **Yes**   |

#### `shell` Job Type Variables

These variables are required when `JOB_TYPE_i` is `shell`.

| Variable                   | Description                                                                                               | Required? |
| -------------------------- | --------------------------------------------------------------------------------------------------------- | --------- |
| `SHELL_COMMAND_i`          | The shell command to execute.                                                                             | **Yes**   |
| `SHELL_TARGET_CONTAINER_i` | The name of the target Docker container to run the command in. If empty, the command runs locally inside the cron-runner container. | No |

## Configuration Examples

Here are some complete examples you can adapt.

### Example 1: Single HTTP Job

Ping a health check endpoint every 5 minutes.

```env
# .env.http
JOB_NAME_1="API Health Check"
CRON_SCHEDULE_1="*/5 * * * *"
JOB_TYPE_1="http"
CRON_TARGET_URL_1="https://api.myapp.com/health"
CRON_SECRET_1="some_very_secret_key"
```

```bash
docker run -d --env-file .env.http darmat1/easypanel-cron
```

### Example 2: Local Shell Command Job

Run a simple `echo` command inside the cron-runner's own container every minute.

```env
# .env.local-shell
JOB_NAME_1="Local Echo"
CRON_SCHEDULE_1="* * * * *"
JOB_TYPE_1="shell"
SHELL_COMMAND_1="echo 'Hello from the cron container! The date is $(date)'"
```

```bash
docker run -d --env-file .env.local-shell darmat1/easypanel-cron
```

### Example 3: Remote Shell Command (in another container)

Run a Laravel Artisan command (`php artisan schedule:run`) in your application's container every minute. This is a primary use case for this tool.

**`docker-compose.yml` or Easypanel "Mounts" setting:**
You must mount the Docker socket to the cron-runner service.

```yaml
services:
  app:
    # ... your application config
  cron:
    image: darmat1/easypanel-cron
    environment:
      - JOB_NAME_1=Laravel Scheduler
      - CRON_SCHEDULE_1=* * * * *
      - JOB_TYPE_1=shell
      - SHELL_TARGET_CONTAINER_1=my-laravel-app-container-name # <-- IMPORTANT
      - SHELL_COMMAND_1=php artisan schedule:run
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

### Example 4: Multiple Jobs Combined

This example configures two jobs:
1.  A daily database backup (remote shell command).
2.  A cache clearing task every hour (HTTP request).

```env
# .env.multi-job

# --- Job 1: Database Backup ---
JOB_NAME_1="Database Backup"
CRON_SCHEDULE_1="0 2 * * *" # Every day at 2:00 AM
JOB_TYPE_1="shell"
SHELL_TARGET_CONTAINER_1="my-postgres-db"
SHELL_COMMAND_1="pg_dump -U myuser -d mydb | gzip > /backups/backup_$(date +\%Y-\%m-\%d).sql.gz"

# --- Job 2: Clear Application Cache ---
JOB_NAME_2="Clear Cache"
CRON_SCHEDULE_2="0 * * * *" # Every hour at minute 0
JOB_TYPE_2="http"
CRON_TARGET_URL_2="https://myapp.com/api/internal/clear-cache"
CRON_SECRET_2="another_strong_secret"
```

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --env-file .env.multi-job \
  darmat1/easypanel-cron
```

## Logging

The application uses Go's standard `slog` library to produce structured JSON logs. This makes them easy to parse, search, and analyze.

**Sample Log Output:**

```json
{"time":"2023-10-27T10:00:00.123Z","level":"INFO","msg":"Starting multi-job CRON runner..."}
{"time":"2023-10-27T10:00:00.124Z","level":"INFO","msg":"Successfully loaded job configuration","job_name":"Database Backup","schedule":"0 2 * * *","type":"shell"}
{"time":"2023-10-27T10:00:00.124Z","level":"INFO","msg":"Successfully loaded job configuration","job_name":"Clear Cache","schedule":"0 * * * *","type":"http"}
{"time":"2023-10-27T10:00:00.125Z","level":"INFO","msg":"CRON scheduler started with configured jobs.","job_count":2}

// Log from an executed job
{"time":"2023-10-27T11:00:00.500Z","level":"INFO","msg":"Executing job","job_name":"Clear Cache","type":"http","target":"https://myapp.com/api/internal/clear-cache"}
{"time":"2023-10-27T11:00:01.200Z","level":"INFO","msg":"Job completed successfully","job_name":"Clear Cache","type":"http","status":"200 OK"}

// Log from a shell command with output
{"time":"2023-10-28T02:00:00.600Z","level":"INFO","msg":"Executing remote shell command via docker exec","job_name":"Database Backup","type":"shell","command":"...","target_container":"my-postgres-db"}
{"time":"2023-10-28T02:00:05.800Z","level":"INFO","msg":"Job completed successfully","job_name":"Database Backup","type":"shell"}
```

## Building from Source

If you want to modify the code, you can build a binary locally.

```bash
# Tidy dependencies
go mod tidy

# Build the binary
go build -o runner main.go
```

## Contributing

Contributions, issues, and feature requests are welcome! Feel free to check the [issues page](https://github.com/darmat1/easypanel-cron/issues).

1.  Fork the Project
2.  Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3.  Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4.  Push to the Branch (`git push origin feature/AmazingFeature`)
5.  Open a Pull Request

## License

Distributed under the MIT License. See `LICENSE` for more information.