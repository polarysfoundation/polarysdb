package config

import (
	"log"
	"os"
	"path/filepath"
)

// getHomeSubDir creates a subdirectory under the user's home directory.
// It takes the subdirectory name and the parent directory name as arguments.
// If the subdirectory does not exist, it will be created with the appropriate permissions.
//
// BEHAVIOR:
//
//   - If `dir` is ABSOLUTE (e.g. /var/lib/myapp/db), the subdir is created
//     directly under `dir`: <dir>/<subdir>. The home directory is NOT
//     prepended. This allows callers to fully control the on-disk layout,
//     which is essential for tests (t.TempDir()) and for production
//     deployments that specify absolute paths.
//
//   - If `dir` is RELATIVE (e.g. "data"), the legacy behavior is preserved:
//     the subdir is created under the user's home directory as
//     ~/<dir>/<subdir>. This keeps backwards compatibility with existing
//     DefaultConfig() usage and applications that rely on home-based
//     storage.
//
// Edge cases:
//   - Empty `dir` with relative semantics → uses home directly.
//   - Empty `subdir` → returns `dir` (resolved) without adding a trailing
//     path component. Useful for callers that want the base dir.
func getHomeSubDir(subdir string, dir string) string {
	// Si dir es absoluto, respetarlo tal cual — sin prepend de home.
	// Esto permite a tests y deployments producción usar paths absolutos
	// sin leakage hacia home/.
	if filepath.IsAbs(dir) {
		var subDirPath string
		if subdir == "" {
			subDirPath = dir
		} else {
			subDirPath = filepath.Join(dir, subdir)
		}

		// Crear el directorio con permisos apropiados. MkdirAll es no-op
		// si ya existe.
		if err := os.MkdirAll(subDirPath, 0700); err != nil {
			log.Fatalf("failed to create %s directory: %v", subDirPath, err)
		}
		return subDirPath
	}

	// Comportamiento legacy: dir relativo → rutea bajo home/.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory: %v", err)
	}

	var subDirPath string
	if subdir == "" {
		subDirPath = filepath.Join(homeDir, dir)
	} else {
		subDirPath = filepath.Join(homeDir, dir, subdir)
	}

	if err := os.MkdirAll(subDirPath, os.ModePerm); err != nil {
		log.Fatalf("failed to create %s directory: %v", subdir, err)
	}

	return subDirPath
}

// GetHomeSubDir is the exported wrapper around getHomeSubDir.
//
// Deprecated notice: the name is misleading now that the function
// respects absolute paths and does NOT always use home. New code should
// prefer calling GetHomeSubDir directly with the convention documented
// above, or better, pass absolute paths to opt out of home-based routing
// entirely.
func GetHomeSubDir(subdir string, dir string) string {
	return getHomeSubDir(subdir, dir)
}

// GetStateDBPath returns the path to the state database file.
// It takes the parent directory name as an argument and constructs the path
// by calling getHomeSubDir to create the necessary subdirectory structure.
//
// Inherits the absolute-vs-relative behavior of getHomeSubDir:
//   - GetStateDBPath("/var/lib/db") → /var/lib/db/state/state.rdb
//   - GetStateDBPath("data")        → ~/data/state/state.rdb
func GetStateDBPath(dir string) string {
	return filepath.Join(getHomeSubDir("state", dir), "state.rdb")
}
