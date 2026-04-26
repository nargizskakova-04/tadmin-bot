package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"admin-bot/internal/domain"
)

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

	text := string(data)
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{{"+k+"}}", v)
	}
	return text, nil
}
