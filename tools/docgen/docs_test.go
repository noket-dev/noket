package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noket-dev/noket/internal/config"
)

// The docs promise that every toml block is a working config.toml. Decoding
// with the real loader catches unknown keys and bad values, which a typo in
// the documentation would otherwise turn into a support question.
func TestDocumentedTOMLParses(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "src", "content", "docs")
	var blocks int

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (filepath.Ext(path) != ".md" && filepath.Ext(path) != ".mdx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, block := range tomlBlocks(string(data)) {
			if block.skip {
				continue
			}
			blocks++
			name := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(name, []byte(block.body), 0o600); err != nil {
				return err
			}
			if _, err := config.LoadFile(name); err != nil {
				t.Errorf("%s:%d: %v", path, block.line, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocks == 0 {
		t.Fatal("no toml blocks found in the docs")
	}
}

type tomlBlock struct {
	line int
	body string
	skip bool
}

// tomlBlocks returns fenced ```toml blocks. A block preceded by an HTML
// comment containing "docs:no-check" is skipped, for examples that are
// deliberately invalid.
func tomlBlocks(src string) []tomlBlock {
	lines := strings.Split(src, "\n")
	var (
		blocks []tomlBlock
		body   []string
		start  int
		recent []string
	)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start == 0 {
			if trimmed == "```toml" {
				start = i + 1
				body = nil
			} else {
				recent = append(recent, line)
				if len(recent) > 3 {
					recent = recent[1:]
				}
			}
			continue
		}
		if trimmed == "```" {
			skip := false
			for _, prev := range recent {
				if strings.Contains(prev, "docs:no-check") {
					skip = true
				}
			}
			blocks = append(blocks, tomlBlock{
				line: start,
				body: strings.Join(body, "\n") + "\n",
				skip: skip,
			})
			start = 0
			recent = nil
			continue
		}
		body = append(body, line)
	}
	return blocks
}

func TestTOMLBlocksSkipMarker(t *testing.T) {
	src := "text\n\n<!-- docs:no-check -->\n```toml\nbad = \n```\n\n```toml\nok = 1\n```\n"
	blocks := tomlBlocks(src)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if !blocks[0].skip {
		t.Errorf("first block at line %d should be skipped", blocks[0].line)
	}
	if blocks[1].skip {
		t.Error("second block should be checked")
	}
}
