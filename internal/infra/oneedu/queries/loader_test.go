package queries

import (
	"strings"
	"sync"
	"testing"
)

// TestLoadOperation_KnownOps verifies every operation in raids.graphql loads
// and the returned text actually starts with the expected operation name.
func TestLoadOperation_KnownOps(t *testing.T) {
	cases := []struct {
		op       string
		wantKind string // "query" or "mutation"
	}{
		{"GetCurrentPiscineId", "query"},
		{"GetRaidByName", "query"},
		{"GetRaidsByPiscineGoId", "query"},
		{"GetRaidsByPiscineJsId", "query"},
		{"GetRaidsByPiscineAiId", "query"},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			got, err := LoadOperation("raids.graphql", tc.op)
			if err != nil {
				t.Fatalf("LoadOperation(%q) error: %v", tc.op, err)
			}
			if !strings.HasPrefix(got, tc.wantKind+" "+tc.op) {
				t.Errorf("LoadOperation(%q) did not start with %q %q: got %q",
					tc.op, tc.wantKind, tc.op, firstLine(got))
			}
			// Body should contain at least one brace.
			if !strings.Contains(got, "{") || !strings.Contains(got, "}") {
				t.Errorf("LoadOperation(%q) returned text without braces", tc.op)
			}
		})
	}
}

func TestLoadOperation_UnknownOperation(t *testing.T) {
	_, err := LoadOperation("raids.graphql", "NoSuchOp")
	if err == nil {
		t.Fatal("expected error for unknown operation, got nil")
	}
	if !strings.Contains(err.Error(), "NoSuchOp") {
		t.Errorf("error message should mention the missing op: %v", err)
	}
}

func TestLoadOperation_UnknownFile(t *testing.T) {
	_, err := LoadOperation("does_not_exist.graphql", "Anything")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadOperation_BodiesAreDistinct(t *testing.T) {
	// Regression for the previous bug: a single global cache keyed only by
	// operation name could mix bodies across files. Ensure each operation in
	// raids.graphql has a body unique enough not to collide.
	a, err := LoadOperation("raids.graphql", "GetCurrentPiscineId")
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOperation("raids.graphql", "GetRaidByName")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two distinct operations returned the same body")
	}
}

func TestLoadOperation_ConcurrentSafe(t *testing.T) {
	// Forces concurrent first-load. Run with -race to catch issues.
	resetCacheForTest()

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := LoadOperation("raids.graphql", "GetCurrentPiscineId"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent load error: %v", err)
	}
}

// resetCacheForTest clears the package-level cache so concurrent-load tests
// exercise the first-parse path. Not exported.
func resetCacheForTest() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	parsedFile = map[string]map[string]string{}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
