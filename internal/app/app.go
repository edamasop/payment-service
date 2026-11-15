package app

import (
	"context"
	"errors"
	"net/http"
	"payment-service/internal/config"
	delivery "payment-service/internal/delivery/http"
	"payment-service/internal/repository"
	"payment-service/internal/server"
	"payment-service/internal/service"

	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/edamasop/events"
	"github.com/edamasop/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

func Run() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := config.Load()

	log := logrus.New()
	log.SetOutput(os.Stdout)
	entry := logrus.NewEntry(log)
	startedAt := time.Now()

	// 1. Initialize PostgreSQL
	dbPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Unable to connect to PostgreSQL: %v", err)
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctx); err != nil {
		log.Fatalf("PostgreSQL ping failed: %v", err)
	}
	log.Info("Successfully connected to PostgreSQL")

	repositories := repository.NewRepositories(dbPool)
	services := service.NewServices(cfg, repositories, log)

	// 2. Consume the order stream: a new order starts its payment.
	consumer, err := messaging.NewKafkaConsumer(messaging.ConsumerConfig{
		BootstrapServers: strings.Join(cfg.KafkaBrokers, ","),
		GroupID:          cfg.KafkaGroupID,
		Topics:           cfg.KafkaConsumerTopics,
		EnableLogging:    false,
		LogOutput:        os.Stdout,
		ManualCommit:     true,
		MaxRetries:       3,
		ErrorBackoff:     2 * time.Second,
		ReadTimeout:      5 * time.Second,

		EnableDLQ: true,
		DLQTopic:  cfg.KafkaProducerTopic + ".dlq",

		HealthCheckInterval:       15 * time.Second,
		UnhealthyFailureThreshold: 5,
		HealthFailureWindow:       1 * time.Minute,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}

	orderConsumer := service.NewOrderConsumer(services.Payment, entry)
	consumer.RegisterHandler(string(events.OrderCreated), orderConsumer.HandleOrderCreated)

	go func() {
		log.Info("Starting message consumption engine...")
		consumer.Start(ctx)
	}()

	// 3. Publish payment events through the transactional outbox.
	producer, err := messaging.NewKafkaProducer(messaging.ProducerConfig{
		BootstrapServers:       strings.Join(cfg.KafkaBrokers, ","),
		Topic:                  cfg.KafkaProducerTopic,
		EnableLogging:          false,
		LogOutput:              os.Stdout,
		ErrOutput:              os.Stderr,
		MaxAttempts:            3,
		AllowAutoTopicCreation: true,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}

	log.Info("Successfully connected to Kafka producer")

	outboxPoller := service.NewOutboxPoller(repositories.Outbox, producer, entry)
	outboxPoller.Start(ctx)

	handlerChain := delivery.NewRouter(delivery.NewHandlers(services), delivery.Options{
		AllowedOrigins: cfg.AllowedOrigins,
		Monitoring: delivery.MonitoringDeps{
			Service:  "payment-service",
			DB:       dbPool,
			Consumer: consumer,
			Producer: producer,
			Outbox:   outboxPoller,
			Log:      entry,
			Started:  startedAt,
		},
	})

	svr, err := server.NewServer(cfg, handlerChain)
	if err != nil {
		log.Fatalf("Unable to init svr: %v", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("Successfully started server on port: ", cfg.Port)
		serverErr <- svr.Run()
	}()

	select {
	case <-ctx.Done():
		log.Info("Shutting down signal received...")
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Error("HTTP server stopped unexpectedly")
		}
	}

	// Stop the background loops before the resources they use are closed.
	// cancel() also covers the "server failed" path, where the signal context
	// is still alive.
	cancel()

	// 1. Stop accepting new requests and let in-flight ones finish. A fresh
	// context is required: the signal context is already cancelled, so passing
	// it here would abort the drain immediately.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := svr.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("graceful HTTP shutdown failed")
	}

	// 2. Let the poller finish the batch it is currently publishing.
	outboxPoller.Wait()

	// 3. Flush the producer, then stop consuming.
	if err := producer.Close(); err != nil {
		log.WithError(err).Error("failed to close Kafka producer")
	}
	if err := consumer.Close(); err != nil {
		log.WithError(err).Error("failed to close Kafka consumer")
	}

	log.Info("shutdown complete")
}
