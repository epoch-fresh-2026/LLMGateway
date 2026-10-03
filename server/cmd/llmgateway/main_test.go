package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"LLMGateway/server/internal/config"
)

func TestRunEnablesConfiguredDebugLogging(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	t.Setenv(config.EnvLogLevel, "DEBUG")
	t.Setenv(config.EnvDatabaseURL, "")
	if err := run(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run error = %v", err)
	}
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("LOG_LEVEL=DEBUG did not enable runtime logger")
	}
}

func TestBuildStoreRequiresDatabaseURL(t *testing.T) {
	st, cipher, closeStore, err := buildStore(context.Background(), config.Config{})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("buildStore error = %v, want missing DATABASE_URL error", err)
	}
	if st != nil || cipher != nil || closeStore != nil {
		t.Fatalf("buildStore returned non-nil resources on invalid config: store=%t cipher=%t closeStore=%t", st != nil, cipher != nil, closeStore != nil)
	}
}

func TestBuildStoreValidatesChannelKeyBeforeConnecting(t *testing.T) {
	st, cipher, closeStore, err := buildStore(context.Background(), config.Config{
		DatabaseURL: "postgres://127.0.0.1:1/unused?sslmode=disable",
	})
	if err == nil || !strings.Contains(err.Error(), "channel key encryption") {
		t.Fatalf("buildStore error = %v, want channel key encryption error", err)
	}
	if st != nil || cipher != nil || closeStore != nil {
		t.Fatalf("buildStore returned non-nil resources on invalid config: store=%t cipher=%t closeStore=%t", st != nil, cipher != nil, closeStore != nil)
	}
}
