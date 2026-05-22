package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"admin-bot/internal/domain"
)

// placeholderRe matches a single {{KEY}} placeholder. KEY is one or more
// word characters (letters/digits/underscore), which covers every key the
// strategy layer produces (RAID_NAME, TEAMS_COUNT, ROWS, ...).
var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

type FileLoader struct {
	root string
}

func NewFileLoader(root string) *FileLoader {
	return &FileLoader{root: root}
}

func (l *FileLoader) Render(key string, vars map[string]string) (string, error) {
	path := filepath.Join(l.root, key+".txt")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", domain.ErrTemplateNotFound, key)
		}
		return "", fmt.Errorf("read template %q: %w", key, err)
	}

	// Single-pass substitution. A value that happens to look like another
	// placeholder is NOT re-expanded — this is intentional, both to keep the
	// loader O(n) and to avoid map-iteration-order-dependent output.
	return placeholderRe.ReplaceAllStringFunc(string(data), func(match string) string {
		// match is "{{KEY}}". Strip the braces to get the key.
		name := match[2 : len(match)-2]
		if v, ok := vars[name]; ok {
			return v
		}
		// Unknown vars stay as literal placeholders, so message authors can
		// spot them at runtime.
		return match
	}), nil
}
