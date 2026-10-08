package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL migration coverage")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestRunRejectsNilAndClosedPools(t *testing.T) {
	if err := Run(t.Context(), nil, ""); err == nil || !strings.Contains(err.Error(), "nil pool") {
		t.Fatalf("nil pool = %v", err)
	}
	pool := migrationPool(t)
	pool.Close()
	if err := Run(t.Context(), pool, ""); err == nil || !strings.Contains(err.Error(), "acquire connection") {
		t.Fatalf("closed pool = %v", err)
	}
}

func TestRunAppliesRealMigrationsAndIsIdempotent(t *testing.T) {
	pool := migrationPool(t)
	for i := 0; i < 2; i++ {
		if err := Run(t.Context(), pool, "../../../db/migrations"); err != nil {
			t.Fatal(err)
		}
	}
	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass('public.sessions') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Fatalf("session table exists=%v, error=%v", exists, err)
	}
}

func TestRunReturnsMigrationLoadAndSQLFailures(t *testing.T) {
	pool := migrationPool(t)
	if err := Run(t.Context(), pool, "../../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), pool, t.TempDir()+"/missing"); err == nil || !strings.Contains(err.Error(), "load migrations") {
		t.Fatalf("load error = %v", err)
	}
	var before int32
	if err := pool.QueryRow(t.Context(), "SELECT version FROM public.schema_version").Scan(&before); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files, err := os.ReadDir("../../../db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join("../../../db/migrations", file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file.Name()), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d_failure.sql", before+1)), []byte("SELECT nonexistent_coverage_migration_function();"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), pool, dir); err == nil || !strings.Contains(err.Error(), "run tern") {
		t.Fatalf("SQL migration error = %v", err)
	}
	var after int32
	if err := pool.QueryRow(t.Context(), "SELECT version FROM public.schema_version").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("failed migration advanced version %d -> %d", before, after)
	}
}

func TestRunReturnsInitializationLockTimeout(t *testing.T) {
	pool := migrationPool(t)
	if err := Run(t.Context(), pool, "../../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	locked, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(context.Background())
	// tern v2.3.3 acquires this advisory lock during initialization before
	// inspecting schema_version; contention must fail before Migrate is called.
	if _, err := locked.Exec(t.Context(), "SELECT pg_advisory_xact_lock($1)", int64(9628173550095224)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := Run(ctx, pool, "../../../db/migrations"); err == nil || !strings.Contains(err.Error(), "initialize tern") {
		t.Fatalf("initialization error = %v", err)
	}
}
