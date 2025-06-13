# --- Этап 1: Сборка приложения (Builder) ---
  FROM golang:1.21-alpine AS builder

  WORKDIR /app
  
  COPY go.mod go.sum ./
  RUN go mod download
  
  COPY . .
  
  # Собираем статичный бинарный файл
  RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /runner main.go
  
  
  # --- Этап 2: Финальный образ (Runner) ---
  # Используем distroless образ, который содержит только наше приложение и его зависимости.
  # Он не содержит shell, менеджера пакетов и других утилит.
  FROM gcr.io/distroless/static-debian11
  
  # Копируем системные сертификаты для HTTPS. Distroless их не имеет по умолчанию.
  COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
  
  # Копируем наш скомпилированный бинарный файл.
  COPY --from=builder /runner /runner
  
  # Указываем команду для запуска.
  ENTRYPOINT ["/runner"]