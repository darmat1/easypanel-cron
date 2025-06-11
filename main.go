package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
)

// Config хранит всю конфигурацию, полученную из переменных окружения.
type Config struct {
	Schedule    string
	TargetURL   string
	SecretToken string
}

// loadConfig загружает и проверяет переменные окружения.
func loadConfig() (*Config, error) {
	schedule := os.Getenv("CRON_SCHEDULE")
	if schedule == "" {
		schedule = "0 */6 * * *" // Значение по умолчанию
	}

	targetURL := os.Getenv("CRON_TARGET_URL")
	if targetURL == "" {
		return nil, errors.New("CRON_TARGET_URL environment variable is not set")
	}

	secretToken := os.Getenv("CRON_SECRET")
	if secretToken == "" {
		return nil, errors.New("CRON_SECRET environment variable is not set")
	}

	return &Config{
		Schedule:    schedule,
		TargetURL:   targetURL,
		SecretToken: secretToken,
	}, nil
}

// === ПРОСТОЙ АДАПТЕР ДЛЯ ЛОГГИРОВАНИЯ ===
// Он удовлетворяет интерфейсу cron.Logger и использует наш slog.Logger
type SlogCronLogger struct {
	Logger *slog.Logger
}

func (s SlogCronLogger) Info(msg string, keysAndValues ...interface{}) {
	s.Logger.Info(msg, keysAndValues...)
}

func (s SlogCronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	// === ИСПРАВЛЕНИЕ 2: ПРАВИЛЬНЫЙ ВЫЗОВ slog.Error ===
	// Мы передаем все аргументы как одну последовательность ключ-значение.
	args := append([]interface{}{"error", err}, keysAndValues...)
	s.Logger.Error(msg, args...)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("Starting CRON runner...")

	config, err := loadConfig()
	if err != nil {
		logger.Error("Failed to load configuration", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("Configuration loaded", "schedule", config.Schedule, "targetURL", config.TargetURL)

	// Создаем функцию-задачу
	job := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		logger.Info("Executing CRON job", "target", config.TargetURL)

		req, err := http.NewRequestWithContext(ctx, "GET", config.TargetURL, nil)
		if err != nil {
			logger.Error("Failed to create request", "error", err.Error())
			return
		}

		req.Header.Set("Authorization", "Bearer "+config.SecretToken)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			logger.Error("Failed to execute request", "error", err.Error())
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			logger.Error("Request failed with non-2xx status code", "status", resp.Status)
			return
		}

		logger.Info("CRON job completed successfully", "status", resp.Status)
	}

	// === ИСПРАВЛЕНИЕ 1: ИСПОЛЬЗУЕМ НАШ АДАПТЕР ===
	cronLogger := SlogCronLogger{Logger: logger}
	c := cron.New(cron.WithChain(cron.Recover(cronLogger))) // Recover - чтобы крон не падал при панике в задаче

	_, err = c.AddFunc(config.Schedule, job)
	if err != nil {
		logger.Error("Failed to add CRON job", "error", err.Error())
		os.Exit(1)
	}
	c.Start()

	logger.Info("CRON scheduler started. Waiting for signals to shut down.")

	// Ожидаем сигнала для graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down CRON runner...")
	shutdownCtx := c.Stop()
	<-shutdownCtx.Done()
	logger.Info("CRON runner shut down gracefully.")
}
