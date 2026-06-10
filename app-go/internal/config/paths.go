package config

import (
	"errors"
	"os"
	"path/filepath"
)

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if exists(filepath.Join(dir, "AGENTS.md")) && exists(filepath.Join(dir, "static")) && exists(filepath.Join(dir, "workflows")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not locate Infinite-Canvas repo root")
		}
		dir = parent
	}
}

func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
