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

// Config хранит конфигурацию для ОДНОЙ задачи.
type Config struct {
	Name     string // Имя задачи для логирования
	Schedule string
	JobType  string // "http" или "shell"

	// Поля для типа "http"
	TargetURL   string
	SecretToken string

	// Поля для типа "shell"
	ShellCommand         string
	ShellTargetContainer string
}

// loadConfigs загружает конфигурации для ВСЕХ задач из переменных окружения.
func loadConfigs(logger *slog.Logger) []Config {
	var configs []Config

	// Ищем задачи в бесконечном цикле, пока находим CRON_SCHEDULE_i
	for i := 1; ; i++ {
		scheduleKey := fmt.Sprintf("CRON_SCHEDULE_%d", i)
		schedule := os.Getenv(scheduleKey)

		// Если расписание для текущего индекса не найдено, считаем, что задач больше нет.
		if schedule == "" {
			break
		}

		jobType := os.Getenv(fmt.Sprintf("JOB_TYPE_%d", i))
		if jobType == "" {
			jobType = "http" // Тип по умолчанию
		}

		jobName := os.Getenv(fmt.Sprintf("JOB_NAME_%d", i))
		if jobName == "" {
			jobName = fmt.Sprintf("job_#%d", i) // Имя по умолчанию
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
			continue // Пропускаем эту задачу и переходим к следующей
		}

		configs = append(configs, config)
		logger.Info("Successfully loaded job configuration", "job_name", config.Name, "schedule", config.Schedule, "type", config.JobType)
	}

	return configs
}

// SlogCronLogger (без изменений)
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("Starting multi-job CRON runner...")

	configs := loadConfigs(logger)
	if len(configs) == 0 {
		logger.Warn("No valid jobs configured. Exiting.")
		os.Exit(0)
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	cronLogger := SlogCronLogger{Logger: logger}
	c := cron.New(cron.WithChain(cron.Recover(cronLogger)))

	// Итерируемся по всем загруженным конфигурациям и создаем для каждой свою задачу
	for _, config := range configs {
		// ВАЖНО: Создаем копию переменной `config` для замыкания.
		// Это предотвращает использование последней конфигурации из цикла для всех задач.
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
					log.Info("Executing local shell command")
					cmd = exec.CommandContext(ctx, "sh", "-c", jobConf.ShellCommand)
				} else {
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

		// Добавляем созданную задачу в планировщик
		_, err := c.AddFunc(jobConf.Schedule, job)
		if err != nil {
			logger.Error("Failed to add CRON job", "job_name", jobConf.Name, "error", err)
		}
	}

	c.Start()
	logger.Info("CRON scheduler started with configured jobs.", "job_count", len(c.Entries()))

	// Graceful Shutdown (без изменений)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down CRON runner...")
	shutdownCtx := c.Stop()
	<-shutdownCtx.Done()
	logger.Info("CRON runner shut down gracefully.")
}
