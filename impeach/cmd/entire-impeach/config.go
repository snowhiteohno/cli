package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// config is the committed .impeach.json. It exists because the security
// policy requires the test command to come from a flag or from a committed
// file, never from a transcript.
type config struct {
	// Test is the test command to rerun.
	Test string `json:"test"`
	// Setup runs before the tests in each worktree. Its output never
	// contributes test ids, so it is how a detached worktree builds whatever
	// the suite needs.
	Setup string `json:"setup"`
	// Sensitive forbids anything leaving the machine. Committing it means the
	// repository itself declares the constraint, so a colleague who runs
	// Impeach here cannot enable a model command by accident.
	Sensitive bool `json:"sensitive"`
}

// configName is looked for at the repository root.
const configName = ".impeach.json"

// loadConfig reads .impeach.json from the repository root. A missing file is
// not an error; it just means the rerun needs --test.
func loadConfig(repo string) (*config, error) {
	path := filepath.Join(repo, configName)
	blob, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &config{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c config
	if err := json.Unmarshal(blob, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}
