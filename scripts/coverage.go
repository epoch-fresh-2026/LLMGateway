// Run from server: go run ../scripts/coverage.go. TEST_DATABASE_URL must point
// to a disposable database: integration fixtures truncate their tables.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type packageInfo struct {
	ImportPath                                   string
	Dir                                          string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
}

type coverageBlock struct {
	Location         string
	File             string
	Statements, Hits int64
}

type counts struct {
	Covered int64 `json:"covered"`
	Total   int64 `json:"total"`
}

type coverageReport struct {
	counts
	Files      map[string]counts `json:"files"`
	Inventory  []string          `json:"inventory"`
	Missing    []string          `json:"missing"`
	Uncovered  []string          `json:"uncovered"`
	Exclusions []string          `json:"exclusions"`
}

var blockPattern = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)

// A block can occur once per test binary with -coverpkg. Coverage is the union
// of those executions; duplicated blocks must not inflate the denominator.
func parseProfile(r io.Reader) ([]coverageBlock, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() || scanner.Text() != "mode: atomic" {
		return nil, errors.New("expected an atomic Go coverage profile")
	}
	byLocation := map[string]coverageBlock{}
	for scanner.Scan() {
		line := scanner.Text()
		parts := blockPattern.FindStringSubmatch(line)
		if parts == nil {
			return nil, fmt.Errorf("invalid coverage block: %q", line)
		}
		statements, err := strconv.ParseInt(parts[6], 10, 64)
		if err != nil {
			return nil, err
		}
		hits, err := strconv.ParseInt(parts[7], 10, 64)
		if err != nil {
			return nil, err
		}
		location := line[:strings.LastIndex(line[:strings.LastIndex(line, " ")], " ")]
		block := coverageBlock{Location: location, File: parts[1], Statements: statements, Hits: hits}
		if previous, exists := byLocation[location]; exists {
			if previous.Statements != statements {
				return nil, fmt.Errorf("inconsistent statement count: %s", location)
			}
			if previous.Hits > block.Hits {
				block.Hits = previous.Hits
			}
		}
		byLocation[location] = block
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(byLocation) == 0 {
		return nil, errors.New("empty coverage profile")
	}
	blocks := make([]coverageBlock, 0, len(byLocation))
	for _, block := range byLocation {
		blocks = append(blocks, block)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Location < blocks[j].Location })
	return blocks, nil
}

func productionPackage(path string) bool {
	return !strings.HasSuffix(path, "/internal/db/sqlc") && !strings.Contains(path, "/internal/db/sqlc/") && !strings.Contains(path, "/internal/testutil/") && !strings.HasSuffix(path, "/internal/testutil")
}

func sourceInventory(packages []packageInfo) ([]string, error) {
	var inventory []string
	for _, pkg := range packages {
		if !productionPackage(pkg.ImportPath) {
			continue
		}
		for _, name := range append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...) {
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg.Dir, name), nil, 0)
			if err != nil {
				return nil, err
			}
			executable := false
			ast.Inspect(file, func(node ast.Node) bool {
				if block, ok := node.(*ast.BlockStmt); ok && len(block.List) > 0 {
					executable = true
				}
				return true
			})
			if executable {
				inventory = append(inventory, pkg.ImportPath+"/"+name)
			}
		}
	}
	sort.Strings(inventory)
	return inventory, nil
}

func summarize(blocks []coverageBlock, inventory []string) (coverageReport, error) {
	report := coverageReport{Files: map[string]counts{}, Inventory: inventory, Missing: []string{}, Uncovered: []string{}, Exclusions: []string{"internal/db/sqlc (generated)", "internal/testutil (test support)"}}
	for _, block := range blocks {
		if !productionPackage(block.File) {
			return report, fmt.Errorf("excluded source was instrumented: %s", block.File)
		}
		file := report.Files[block.File]
		file.Total += block.Statements
		report.Total += block.Statements
		if block.Hits > 0 {
			file.Covered += block.Statements
			report.Covered += block.Statements
		} else if block.Statements > 0 {
			report.Uncovered = append(report.Uncovered, block.Location)
		}
		report.Files[block.File] = file
	}
	for _, path := range inventory {
		if _, exists := report.Files[path]; !exists {
			report.Missing = append(report.Missing, path)
		}
	}
	return report, nil
}

func requireComplete(report coverageReport) error {
	if report.Total == 0 {
		return errors.New("coverage contains zero production statements")
	}
	if len(report.Missing) > 0 {
		return fmt.Errorf("runtime source missing from coverage: %s", strings.Join(report.Missing, ", "))
	}
	if report.Covered != report.Total {
		return fmt.Errorf("%d uncovered statements: %s", report.Total-report.Covered, strings.Join(report.Uncovered, ", "))
	}
	return nil
}

func runGo(args ...string) error {
	command := exec.Command("go", args...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

func execute() error {
	profile := flag.String("profile", "coverage/backend.out", "merged coverage output path (relative to server)")
	race := flag.Bool("race", false, "enable the Go race detector during coverage tests")
	flag.Parse()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		return errors.New("TEST_DATABASE_URL is required; use a disposable PostgreSQL database")
	}
	command := exec.Command("go", "list", "-json", "./...")
	command.Stderr = os.Stderr
	listing, err := command.Output()
	if err != nil {
		return err
	}
	var packages []packageInfo
	decoder := json.NewDecoder(bytes.NewReader(listing))
	for {
		var pkg packageInfo
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		packages = append(packages, pkg)
	}
	inventory, err := sourceInventory(packages)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*profile), 0755); err != nil {
		return err
	}
	var instrumented, tested []string
	for _, pkg := range packages {
		if productionPackage(pkg.ImportPath) {
			instrumented = append(instrumented, pkg.ImportPath)
		}
		if len(pkg.TestGoFiles)+len(pkg.XTestGoFiles) > 0 {
			tested = append(tested, pkg.ImportPath)
		}
	}
	// Always regenerate the profile: filename-only inventory checks cannot
	// establish that an old profile covers new statements in an existing file.
	// Database suites reset fixtures, so binaries share the database sequentially.
	args := []string{"test", "-count=1", "-p=1", "-covermode=atomic", "-coverpkg=" + strings.Join(instrumented, ","), "-coverprofile=" + *profile}
	if *race {
		args = append(args, "-race")
	}
	args = append(args, tested...)
	if err := runGo(args...); err != nil {
		return err
	}
	raw, err := os.ReadFile(*profile)
	if err != nil {
		return err
	}
	blocks, err := parseProfile(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	var normalized strings.Builder
	normalized.WriteString("mode: atomic\n")
	for _, block := range blocks {
		fmt.Fprintf(&normalized, "%s %d %d\n", block.Location, block.Statements, block.Hits)
	}
	if err := os.WriteFile(*profile, []byte(normalized.String()), 0644); err != nil {
		return err
	}
	report, err := summarize(blocks, inventory)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*profile+".json", append(encoded, '\n'), 0644); err != nil {
		return err
	}
	if err := runGo("tool", "cover", "-html="+*profile, "-o="+*profile+".html"); err != nil {
		return err
	}
	fmt.Printf("Exact backend production coverage: %d/%d statements; %d executable source files; %d missing files; %d uncovered statements.\n", report.Covered, report.Total, len(inventory), len(report.Missing), report.Total-report.Covered)
	return requireComplete(report)
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
