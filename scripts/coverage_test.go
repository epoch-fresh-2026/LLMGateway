package main

import (
	"strings"
	"testing"
)

func TestCoverageUnionAndStrictGate(t *testing.T) {
	profile := "mode: atomic\napp/main.go:1.1,2.2 2 0\napp/main.go:1.1,2.2 2 1\napp/main.go:3.1,4.2 1 0\n"
	blocks, err := parseProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatal(err)
	}
	report, err := summarize(blocks, []string{"app/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Covered != 2 || report.Total != 3 {
		t.Fatalf("union = %+v", report)
	}
	if requireComplete(report) == nil {
		t.Fatal("uncovered statement passed the strict gate")
	}
	blocks[1].Hits = 1
	report, err = summarize(blocks, []string{"app/main.go", "app/unimported.go"})
	if err != nil {
		t.Fatal(err)
	}
	if requireComplete(report) == nil {
		t.Fatal("unimported runtime source passed the inventory gate")
	}
	report, err = summarize(blocks, []string{"app/main.go"})
	if err != nil || requireComplete(report) != nil {
		t.Fatalf("complete coverage rejected: %+v/%v", report, err)
	}
}

func TestMalformedAndInconsistentProfilesFail(t *testing.T) {
	for _, profile := range []string{
		"mode: atomic\n",
		"mode: set\napp/main.go:1.1,2.2 1 1\n",
		"mode: atomic\ninvalid\n",
		"mode: atomic\napp/main.go:1.1,2.2 1 1\napp/main.go:1.1,2.2 2 1\n",
	} {
		if _, err := parseProfile(strings.NewReader(profile)); err == nil {
			t.Fatalf("invalid profile accepted: %q", profile)
		}
	}
	if requireComplete(coverageReport{}) == nil {
		t.Fatal("zero denominator accepted")
	}
	if requireComplete(coverageReport{counts: counts{Covered: 999999, Total: 1000000}}) == nil {
		t.Fatal("rounded 100 percent accepted despite an uncovered statement")
	}
}

func TestProductionExclusionsUseDirectoryBoundaries(t *testing.T) {
	for _, path := range []string{"app/internal/db/sqlc", "app/internal/db/sqlc/query.go", "app/internal/testutil/storefake", "app/internal/testutil/app/fake.go"} {
		if productionPackage(path) {
			t.Fatalf("test/generated source included: %s", path)
		}
	}
	for _, path := range []string{"app/cmd/llmgateway/main.go", "app/internal/db/migrate/migrate.go", "app/internal/db/sqlc_helpers/helper.go", "app/internal/testutility/helper.go"} {
		if !productionPackage(path) {
			t.Fatalf("handwritten source excluded: %s", path)
		}
	}
}
