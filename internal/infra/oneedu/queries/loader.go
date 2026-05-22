// Package queries provides embedded GraphQL queries for the 01-edu API.
package queries

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

//go:embed *.graphql
var FS embed.FS

// reQuery matches the start of a named GraphQL query/mutation block.
// Capture group 2 is the operation name.
var reQuery = regexp.MustCompile(`(?s)(query|mutation)\s+(\w+)\s*(\([^)]*\))?\s*\{`)

// fileCache stores parsed operations per file. Concurrent-safe.
var (
	cacheMu    sync.RWMutex
	parsedFile = map[string]map[string]string{}
)

// MustLoad reads the entire .graphql file by name.
func MustLoad(name string) (string, error) {
	data, err := FS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read embedded file %q: %w", name, err)
	}
	return string(data), nil
}

// LoadOperation extracts a single named operation from the .graphql file.
// For example: LoadOperation("raids.graphql", "GetCurrentPiscineId").
func LoadOperation(file, operationName string) (string, error) {
	ops, err := opsFor(file)
	if err != nil {
		return "", err
	}
	q, ok := ops[operationName]
	if !ok {
		return "", fmt.Errorf("operation %q not found in %q", operationName, file)
	}
	return q, nil
}

// opsFor returns the parsed operation map for a file, caching across calls.
func opsFor(file string) (map[string]string, error) {
	cacheMu.RLock()
	ops, ok := parsedFile[file]
	cacheMu.RUnlock()
	if ok {
		return ops, nil
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()
	// Re-check after acquiring the write lock to avoid double-parsing.
	if ops, ok := parsedFile[file]; ok {
		return ops, nil
	}

	ops, err := parseFile(file)
	if err != nil {
		return nil, err
	}
	parsedFile[file] = ops
	return ops, nil
}

// parseFile reads the file and splits it into individual named operations.
func parseFile(file string) (map[string]string, error) {
	raw, err := MustLoad(file)
	if err != nil {
		return nil, err
	}

	matches := reQuery.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no named operations found in %q", file)
	}

	ops := make(map[string]string, len(matches))
	for i, m := range matches {
		// m holds index pairs for [whole, kind, name, params]; the name is group 2.
		nameStart, nameEnd := m[4], m[5]
		opName := raw[nameStart:nameEnd]

		blockStart := m[0]
		blockEnd := len(raw)
		if i+1 < len(matches) {
			blockEnd = matches[i+1][0]
		}
		ops[opName] = strings.TrimSpace(raw[blockStart:blockEnd])
	}

	return ops, nil
}
