package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisibleBackendModuleDirectories(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{
		"internal/catalog",
		"internal/accounts",
		"internal/usage",
		"internal/ratelimit",
		"internal/quota",
		"internal/httpapi",
		"internal/httpcommon",
		"internal/proxy",
		"internal/proxy/openai",
		"internal/store/postgres",
		"internal/db/sqlc",
	} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Fatalf("expected module directory %s to exist", dir)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "internal/handler")); !os.IsNotExist(err) {
		t.Fatalf("internal/handler should not remain as the HTTP catch-all module")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/domain")); !os.IsNotExist(err) {
		t.Fatalf("internal/domain should not remain after ownership migration")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/store/memory")); !os.IsNotExist(err) {
		t.Fatalf("internal/store/memory should not remain as a production store")
	}
}

func TestBusinessModulesDependOnlyOnStorePorts(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{
		"internal/catalog",
		"internal/accounts",
		"internal/usage",
		"internal/ratelimit",
		"internal/quota",
		"internal/proxy",
		"internal/httpapi",
	} {
		assertNoImports(t, filepath.Join(root, dir), []string{
			"LLMGateway/server/internal/store",
			"LLMGateway/server/internal/store/postgres",
			"LLMGateway/server/internal/db/sqlc",
			"github.com/jackc/pgx",
		})
	}
}

// TestStoreDoesNotOwnBusinessRules guards the store/primitives split: the
// PostgreSQL adapter must not import leaf capability packages that encode
// business rules (money math, key encryption).
func TestStoreDoesNotOwnBusinessRules(t *testing.T) {
	root := filepath.Join("..", "..")
	assertNoImports(t, filepath.Join(root, "internal/store/postgres"), []string{
		"LLMGateway/server/internal/crypto",
		"LLMGateway/server/internal/money",
	})
}

func TestProductionCodeDoesNotImportTestHarness(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			if strings.Contains(filepath.ToSlash(path), "/internal/testutil/") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				switch strings.Trim(spec.Path.Value, `"`) {
				case "LLMGateway/server/internal/testutil/storefake",
					"LLMGateway/server/internal/testutil/app":
					t.Fatalf("production file %s imports a test-only package", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoImports(t *testing.T, root string, forbidden []string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			for _, prefix := range forbidden {
				if strings.HasPrefix(strings.Trim(spec.Path.Value, `"`), prefix) {
					t.Fatalf("%s imports concrete infrastructure package %s", path, spec.Path.Value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBusinessModuleDocumentationExists(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, module := range []string{"catalog", "accounts", "usage", "ratelimit", "quota"} {
		path := filepath.Join(root, "internal", module, "doc.go")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("business module %s is missing doc.go: %v", module, err)
		}
		if !strings.Contains(string(content), "Package "+module) {
			t.Fatalf("business module %s lacks package ownership documentation", module)
		}
	}
}

func TestProxyOrchestrationStaysOutOfHTTPAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"orchestration.go", "routing.go", "ratelimit.go", "billing.go", "auth.go"} {
		if _, err := os.Stat(filepath.Join(root, "internal", "proxy", name)); err != nil {
			t.Fatalf("expected proxy orchestration file %s in internal/proxy: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, "internal", "httpapi", name)); !os.IsNotExist(err) {
			t.Fatalf("proxy orchestration file %s should not live in internal/httpapi", name)
		}
	}
}

func TestProxyBusinessFilesUseBusinessNames(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "proxy")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "openai_") {
			t.Fatalf("generic proxy business file should not use OpenAI protocol prefix: %s", entry.Name())
		}
	}
}

func TestHTTPAPIDoesNotOwnBusinessHelpers(t *testing.T) {
	root := filepath.Join("..", "..")
	content, err := os.ReadFile(filepath.Join(root, "internal", "httpapi", "handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"func splitPath", "func readJSON", "func ParsePagination", "func (a *Server) result", "func (a *Server) noBody", "func errorResponse"} {
		if strings.Contains(string(content), name) {
			t.Fatalf("httpapi still owns obsolete helper %q", name)
		}
	}
	if !strings.Contains(string(content), "adminMux") {
		t.Fatal("httpapi must delegate admin routing to the module ServeMux")
	}
	if strings.Contains(string(content), "strings.Split") {
		t.Fatal("httpapi must not duplicate path parsing")
	}
}

func TestAdminEntrypointsStayInBusinessModules(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"admin_channel.go", "admin_pricing.go", "channel_upstream.go"} {
		assertFileExists(t, filepath.Join(root, "internal", "catalog", name))
		assertFileMissing(t, filepath.Join(root, "internal", "httpapi", name))
	}
	for _, name := range []string{"admin_key.go", "admin_auth.go"} {
		assertFileExists(t, filepath.Join(root, "internal", "accounts", name))
		assertFileMissing(t, filepath.Join(root, "internal", "httpapi", name))
	}
	assertFileExists(t, filepath.Join(root, "internal", "usage", "admin_usage.go"))
	assertFileExists(t, filepath.Join(root, "internal", "ratelimit", "admin_ratelimit.go"))
	assertFileMissing(t, filepath.Join(root, "internal", "httpapi", "admin_usage.go"))
	assertFileMissing(t, filepath.Join(root, "internal", "httpapi", "admin_ratelimit.go"))
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("did not expect %s", path)
	}
}

func TestHTTPRouteTableStaysCentralized(t *testing.T) {
	root := filepath.Join("..", "..")
	content, err := os.ReadFile(filepath.Join(root, "cmd", "llmgateway", "router.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/healthz", "/admin/", "/v1/"} {
		if !strings.Contains(string(content), route) {
			t.Fatalf("cmd/llmgateway/router.go is missing route %s", route)
		}
	}
}

func TestOpenAIWireTypesStayInProtocolPackage(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{
		"internal/catalog",
		"internal/accounts",
		"internal/usage",
		"internal/ratelimit",
	} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(content), "OpenAIModel") || strings.Contains(string(content), "ChatCompletionRequest") {
				t.Fatalf("OpenAI wire type leaked into %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProxyBusinessDoesNotImportOpenAIWirePackage(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "proxy")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Dir(path) != root || !strings.HasSuffix(path, ".go") {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		if strings.Contains(text, "internal/proxy/openai") || strings.Contains(text, "http.ResponseWriter") || strings.Contains(text, "OpenAIError") {
			t.Fatalf("proxy business file %s leaks protocol or HTTP response concerns", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
