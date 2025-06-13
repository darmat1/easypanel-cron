package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
)

// Config holds the configuration for a SINGLE cron job.
type Config struct {
	Name     string // A friendly name for logging purposes.
	Schedule string
	JobType  string // "http" or "shell"

	// Fields for "http" type
	TargetURL   string
	SecretToken string

	// Fields for "shell" type
	ShellCommand         string
	ShellTargetContainer string
}

// loadConfigs loads configurations for ALL jobs from environment variables.
func loadConfigs(logger *slog.Logger) []Config {
	var configs []Config

	// Search for jobs in an infinite loop, looking for CRON_SCHEDULE_i
	for i := 1; ; i++ {
		scheduleKey := fmt.Sprintf("CRON_SCHEDULE_%d", i)
		schedule := os.Getenv(scheduleKey)

		// If a schedule for the current index is not found, we assume there are no more jobs.
		if schedule == "" {
			break
		}

		jobType := os.Getenv(fmt.Sprintf("JOB_TYPE_%d", i))
		if jobType == "" {
			jobType = "http" // Default job type
		}

		jobName := os.Getenv(fmt.Sprintf("JOB_NAME_%d", i))
		if jobName == "" {
			jobName = fmt.Sprintf("job_#%d", i) // Default job name
		}

		config := Config{
			Name:     jobName,
			Schedule: schedule,
			JobType:  jobType,
		}

		var validationError error

		switch jobType {
		case "http":
			config.TargetURL = os.Getenv(fmt.Sprintf("CRON_TARGET_URL_%d", i))
			config.SecretToken = os.Getenv(fmt.Sprintf("CRON_SECRET_%d", i))
			if config.TargetURL == "" {
				validationError = errors.New("CRON_TARGET_URL is required")
			}
			if config.SecretToken == "" {
				validationError = errors.New("CRON_SECRET is required")
			}
		case "shell":
			config.ShellCommand = os.Getenv(fmt.Sprintf("SHELL_COMMAND_%d", i))
			if config.ShellCommand == "" {
				validationError = errors.New("SHELL_COMMAND is required")
			}
			config.ShellTargetContainer = os.Getenv(fmt.Sprintf("SHELL_TARGET_CONTAINER_%d", i))
		default:
			validationError = errors.New("unknown JOB_TYPE: " + jobType)
		}

		if validationError != nil {
			logger.Error("Skipping invalid job configuration", "job_name", config.Name, "reason", validationError)
			continue // Skip this job and move to the next one
		}

		configs = append(configs, config)
		logger.Info("Successfully loaded job configuration", "job_name", config.Name, "schedule", config.Schedule, "type", config.JobType)
	}

	return configs
}

// SlogCronLogger is an adapter to allow the cron library to use our main slog.Logger.
type SlogCronLogger struct {
	Logger *slog.Logger
}

func (s SlogCronLogger) Info(msg string, keysAndValues ...interface{}) {
	s.Logger.Info(msg, keysAndValues...)
}
func (s SlogCronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	s.Logger.Error(msg, append([]interface{}{"error", err}, keysAndValues...)...)
}

func main() {
	// 1. Set up structured JSON logger.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("Starting multi-job CRON runner...")

	// 2. Load all job configurations from environment variables.
	configs := loadConfigs(logger)
	if len(configs) == 0 {
		logger.Warn("No valid jobs configured. Exiting.")
		os.Exit(0)
	}

	// 3. Create a reusable HTTP client and a new cron scheduler.
	httpClient := &http.Client{Timeout: 60 * time.Second}
	cronLogger := SlogCronLogger{Logger: logger}
	c := cron.New(cron.WithChain(
		// Recover prevents the entire runner from crashing if a job panics.
		cron.Recover(cronLogger),
	))

	// 4. Iterate over all loaded configurations and create a job for each.
	for _, config := range configs {
		// IMPORTANT: Create a local copy of the config variable for the closure.
		// This prevents all jobs from using the last configuration in the loop.
		jobConf := config

		var job func()
		switch jobConf.JobType {
		case "http":
			job = func() {
				log := logger.With("job_name", jobConf.Name, "type", "http")
				log.Info("Executing job", "target", jobConf.TargetURL)
				req, err := http.NewRequest("GET", jobConf.TargetURL, nil)
				if err != nil {
					log.Error("Failed to create request", "error", err)
					return
				}
				req.Header.Set("Authorization", "Bearer "+jobConf.SecretToken)

				resp, err := httpClient.Do(req)
				if err != nil {
					log.Error("Failed to execute request", "error", err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode >= 400 {
					log.Error("Request failed", "status", resp.Status)
					return
				}
				log.Info("Job completed successfully", "status", resp.Status)
			}

		case "shell":
			job = func() {
				log := logger.With("job_name", jobConf.Name, "type", "shell")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()

				var cmd *exec.Cmd
				logFields := []interface{}{"command", jobConf.ShellCommand}

				if jobConf.ShellTargetContainer == "" {
					// Execute the command locally within this container.
					log.Info("Executing local shell command")
					cmd = exec.CommandContext(ctx, "sh", "-c", jobConf.ShellCommand)
				} else {
					// Execute the command in another container via `docker exec`.
					logFields = append(logFields, "target_container", jobConf.ShellTargetContainer)
					log.Info("Executing remote shell command via docker exec", logFields...)
					cmd = exec.CommandContext(ctx, "docker", "exec", jobConf.ShellTargetContainer, "sh", "-c", jobConf.ShellCommand)
				}

				var outb, errb bytes.Buffer
				cmd.Stdout = &outb
				cmd.Stderr = &errb

				err := cmd.Run()
				if outb.Len() > 0 {
					log.Info("Command stdout", "output", strings.TrimSpace(outb.String()))
				}
				if errb.Len() > 0 {
					log.Error("Command stderr", "output", strings.TrimSpace(errb.String()))
				}
				if err != nil {
					log.Error("Shell command failed to execute", "error", err)
					return
				}
				log.Info("Job completed successfully")
			}
		}

		// Add the newly created job to the cron scheduler.
		_, err := c.AddFunc(jobConf.Schedule, job)
		if err != nil {
			logger.Error("Failed to add CRON job", "job_name", jobConf.Name, "error", err)
		}
	}

	// 5. Start the cron scheduler.
	c.Start()
	logger.Info("CRON scheduler started with configured jobs.", "job_count", len(c.Entries()))

	// 6. Set up graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // Block until a signal is received.

	logger.Info("Shutting down CRON runner...")
	// Stop the scheduler and wait for any running jobs to finish.
	shutdownCtx := c.Stop()
	<-shutdownCtx.Done()
	logger.Info("CRON runner shut down gracefully.")
}
