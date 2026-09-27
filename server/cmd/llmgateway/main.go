package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/config"
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/db/migrate"
	"LLMGateway/server/internal/httpapi"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/store/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, cipher, closeStore, err := buildStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeStore()

	apiServer, handler := newRouterServer(st,
		httpapi.WithCipher(cipher),
		httpapi.WithUpstreamTimeout(time.Duration(cfg.UpstreamTimeoutSeconds)*time.Second),
		httpapi.WithUpstreamMaxAttempts(cfg.UpstreamMaxAttempts),
		httpapi.WithMinimumRouteBalance(cfg.ChannelMinRouteBalance),
		httpapi.WithChannelBreakerConfig(catalog.ChannelBreakerConfig{
			FailureThreshold:   cfg.ChannelBreakerFailureThreshold,
			Cooldown:           time.Duration(cfg.ChannelBreakerCooldownSeconds) * time.Second,
			WindowSeconds:      cfg.ChannelBreakerWindowSeconds,
			MinimumSamples:     cfg.ChannelBreakerMinimumSamples,
			ErrorRatePercent:   cfg.ChannelBreakerErrorRatePercent,
			TimeoutRatePercent: cfg.ChannelBreakerTimeoutRatePercent,
		}),
		httpapi.WithQuotaConfig(cfg.QuotaDefaultMaxTokens, time.Duration(cfg.QuotaReservationTTLSeconds)*time.Second),
		httpapi.WithSessionConfig(time.Duration(cfg.SessionTTLSeconds)*time.Second, cfg.SessionCookieSecure, cfg.RegistrationEnabled, cfg.BcryptCost))
	server := &http.Server{Addr: cfg.Addr, Handler: handler}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		runQuotaReaper(ctx, quota.New(st, st.QuotaTx(), time.Now), ratelimit.New(st, time.Now), apiServer, time.Duration(cfg.QuotaReaperIntervalSeconds)*time.Second, cfg.QuotaReaperBatchSize, time.Duration(cfg.ChannelBreakerBucketRetentionSeconds)*time.Second)
	}()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("LLMGateway listening on %s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		stop()
		workers.Wait()
		return err
	case <-ctx.Done():
		log.Print("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = server.Shutdown(shutdownCtx)
	workers.Wait()
	return err
}

func runQuotaReaper(ctx context.Context, quotaServer *quota.Server, rateServer *ratelimit.Server, apiServer *httpapi.Server, interval time.Duration, batchSize int, bucketRetention time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := rateServer.ReapRateLimitReservations(ctx, batchSize); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("reap rate limit reservations: %v", err)
			}
			if _, err := quotaServer.ReapExpiredQuotaReservations(ctx, batchSize); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("reap expired quota reservations: %v", err)
			}
			if _, err := apiServer.ReapChannelHealthBuckets(ctx, bucketRetention); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("reap channel health buckets: %v", err)
			}
			if _, err := apiServer.ReapExpiredSessions(ctx, batchSize); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("reap expired sessions: %v", err)
			}
		}
	}
}

// buildStore constructs the only runtime persistence implementation and the
// channel key cipher owned by the catalog module.
func buildStore(ctx context.Context, cfg config.Config) (*postgres.Store, *crypto.Cipher, func(), error) {
	if cfg.DatabaseURL == "" {
		return nil, nil, nil, fmt.Errorf("DATABASE_URL is required")
	}

	cipher, err := crypto.NewCipher([]byte(cfg.ChannelKeyEncryptionKey))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("channel key encryption: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if err := migrate.Run(ctx, pool, cfg.MigrationsDir); err != nil {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	log.Print("using PostgreSQL store")
	return postgres.New(pool), cipher, pool.Close, nil
}
