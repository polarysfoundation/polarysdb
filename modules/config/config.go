package config

import (
	"log"
	"os"
	"path/filepath"
)

func getHomeSubDir(subdir string, dir string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}
	subDirPath := filepath.Join(homeDir, dir, subdir)

	err = os.MkdirAll(subDirPath, os.ModePerm)
	if err != nil {
		log.Fatalf("failed to create %s directory: %v", subdir, err)
	}

	return subDirPath
}

func GetStateDBPath(dir string) string {
	return filepath.Join(getHomeSubDir("state", dir), "state.rdb")
}
