package config

import (
	"log"
	"os"
	"path/filepath"
)

// getHomeSubDir creates a subdirectory under the user's home directory.
// It takes the subdirectory name and the parent directory name as arguments.
// If the subdirectory does not exist, it will be created with the appropriate permissions.
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

// GetStateDBPath returns the path to the state database file.
// It takes the parent directory name as an argument and constructs the path
// by calling getHomeSubDir to create the necessary subdirectory structure.
func GetStateDBPath(dir string) string {
	return filepath.Join(getHomeSubDir("state", dir), "state.rdb")
}
