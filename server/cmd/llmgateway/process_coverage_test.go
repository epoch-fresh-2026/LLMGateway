package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LLMGateway/server/internal/config"
)

func processDatabase(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for process PostgreSQL coverage")
	}
	return url
}

func processEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvDatabaseURL, processDatabase(t))
	t.Setenv(config.EnvChannelKey, "0123456789abcdef0123456789abcdef")
	t.Setenv("MIGRATIONS_DIR", "../../db/migrations")
	oldLog := slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldLog) })
}

func controlledProcessContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	old := processSignalContext
	processSignalContext = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() { cancel(); processSignalContext = old })
	return ctx, cancel
}

func TestMainReportsStartupFailure(t *testing.T) {
	t.Setenv(config.EnvBcryptCost, "invalid")
	old := fatalProcess
	var fatalError error
	fatalProcess = func(values ...any) { fatalError, _ = values[0].(error) }
	t.Cleanup(func() { fatalProcess = old })
	main()
	if fatalError == nil || !strings.Contains(fatalError.Error(), "config:") {
		t.Fatalf("fatal startup error = %v", fatalError)
	}
}

func TestBuildStoreConnectionAndMigrationFailures(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct{ name, url, dir, want string }{
		{"malformed connection", "://invalid", "", "connect postgres"},
		{"unavailable postgres", "postgres://127.0.0.1:1/unused?sslmode=disable&connect_timeout=1", "", "ping postgres"},
		{"missing migrations", "", t.TempDir() + "/absent", "run migrations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			databaseURL := tc.url
			if tc.name == "missing migrations" {
				databaseURL = processDatabase(t)
			}
			st, cipher, closeStore, err := buildStore(t.Context(), config.Config{DatabaseURL: databaseURL, ChannelKeyEncryptionKey: valid, MigrationsDir: tc.dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if st != nil || cipher != nil || closeStore != nil {
				t.Fatal("failed startup retained resources")
			}
		})
	}
}

func TestRunReturnsOccupiedListenerError(t *testing.T) {
	processEnv(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	t.Setenv("ADDR", listener.Addr().String())
	if err := run(); err == nil || !strings.Contains(err.Error(), "bind") {
		t.Fatalf("listen error = %v", err)
	}
}

func TestRunServesHealthAndShutsDownWorkers(t *testing.T) {
	processEnv(t)
	_, cancel := controlledProcessContext(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	t.Setenv("ADDR", address)
	finished := make(chan error, 1)
	go func() { finished <- run() }()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.After(5 * time.Second)
	for {
		response, requestErr := client.Get("http://" + address + "/healthz")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("health = %d", response.StatusCode)
			}
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("process ended before ready: %v", err)
		case <-deadline:
			t.Fatal("process did not become ready")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join workers")
	}
}

type processHTTPDouble struct {
	cancel        context.CancelFunc
	shutdownError error
	stopped       atomic.Bool
}

func (s *processHTTPDouble) ListenAndServe() error { s.cancel(); return http.ErrServerClosed }
func (s *processHTTPDouble) Shutdown(ctx context.Context) error {
	_, bounded := ctx.Deadline()
	s.stopped.Store(bounded)
	return s.shutdownError
}

func TestRunReturnsShutdownFailure(t *testing.T) {
	processEnv(t)
	_, cancel := controlledProcessContext(t)
	want := errors.New("shutdown deadline")
	server := &processHTTPDouble{cancel: cancel, shutdownError: want}
	old := newHTTPServer
	newHTTPServer = func(_ string, handler http.Handler) httpLifecycle {
		if handler == nil {
			t.Fatal("HTTP router missing")
		}
		return server
	}
	t.Cleanup(func() { newHTTPServer = old })
	if err := run(); !errors.Is(err, want) {
		t.Fatalf("shutdown error = %v", err)
	}
	if !server.stopped.Load() {
		t.Fatal("shutdown context must have a deadline")
	}
}

type processReaperDouble struct {
	cancel   context.CancelFunc
	failures error
	calls    []string
}

func (r *processReaperDouble) ReapRateLimitReservations(context.Context, int) (int, error) {
	r.calls = append(r.calls, "rate")
	return 0, r.failures
}
func (r *processReaperDouble) ReapExpiredQuotaReservations(context.Context, int) (int, error) {
	r.calls = append(r.calls, "quota")
	return 0, r.failures
}
func (r *processReaperDouble) ReapChannelHealthBuckets(context.Context, time.Duration) (int, error) {
	r.calls = append(r.calls, "health")
	return 0, r.failures
}
func (r *processReaperDouble) ReapExpiredSessions(context.Context, int) (int, error) {
	r.calls = append(r.calls, "sessions")
	r.cancel()
	return 0, r.failures
}

func TestReaperReportsEachFailureAndIgnoresCancellation(t *testing.T) {
	for _, failure := range []error{errors.New("storage unavailable"), context.Canceled, nil} {
		ctx, cancel := context.WithCancel(t.Context())
		reaper := &processReaperDouble{cancel: cancel, failures: failure}
		var output bytes.Buffer
		oldOutput := log.Writer()
		log.SetOutput(&output)
		runQuotaReaper(ctx, reaper, reaper, reaper, time.Millisecond, 7, time.Minute)
		log.SetOutput(oldOutput)
		if strings.Join(reaper.calls, ",") != "rate,quota,health,sessions" {
			t.Fatalf("reaper order = %v", reaper.calls)
		}
		if failure != nil && !errors.Is(failure, context.Canceled) {
			for _, text := range []string{"reap rate limit reservations", "reap expired quota reservations", "reap channel health buckets", "reap expired sessions"} {
				if !strings.Contains(output.String(), text) {
					t.Fatalf("missing %q in %s", text, output.String())
				}
			}
		} else if output.Len() != 0 {
			t.Fatalf("unexpected log = %s", output.String())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runQuotaReaper(ctx, nil, nil, nil, 0, 7, time.Minute)
}
