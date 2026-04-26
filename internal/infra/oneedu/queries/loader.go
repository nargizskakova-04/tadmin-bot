// Package queries provides embedded GraphQL queries for the 01-edu API.
package queries

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed *.graphql
var FS embed.FS

// queryCache stores parsed queries so we only parse the file once.
var queryCache map[string]string

// reQuery matches a named GraphQL query/mutation block.
// It captures: "query OperationName(...) { ... }"
var reQuery = regexp.MustCompile(`(?s)(query|mutation)\s+(\w+)\s*(\([^)]*\))?\s*\{`)

// MustLoad reads the entire .graphql file by name.
func MustLoad(name string) (string, error) {
	data, err := FS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read embedded file %q: %w", name, err)
	}
	return string(data), nil
}

// LoadOperation extracts a single named operation from the .graphql file.
// For example: LoadOperation("raids.graphql", "GetCurrentPiscineId")
func LoadOperation(file, operationName string) (string, error) {
	if queryCache == nil {
		if err := parseAll(file); err != nil {
			return "", err
		}
	}

	q, ok := queryCache[operationName]
	if !ok {
		return "", fmt.Errorf("operation %q not found in %q", operationName, file)
	}
	return q, nil
}

// parseAll reads the file and splits it into individual named operations.
func parseAll(file string) error {
	raw, err := MustLoad(file)
	if err != nil {
		return err
	}

	queryCache = make(map[string]string)

	// Find all operation start positions.
	matches := reQuery.FindAllStringIndex(raw, -1)
	if len(matches) == 0 {
		return fmt.Errorf("no named operations found in %q", file)
	}

	for i, loc := range matches {
		// Extract operation name from the match.
		matchStr := raw[loc[0]:loc[1]]
		nameMatch := reQuery.FindStringSubmatch(matchStr)
		if len(nameMatch) < 3 {
			continue
		}
		opName := nameMatch[2]

		// The operation body goes from this match start to the next match start
		// (or end of file for the last one).
		start := loc[0]
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(raw)
		}

		queryCache[opName] = strings.TrimSpace(raw[start:end])
	}

	return nil
}
