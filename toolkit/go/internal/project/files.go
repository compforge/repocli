package project

import "path"

// ManifestEcosystem classifies a regular file, not whether discovery selects its directory.
func ManifestEcosystem(name string) string {
	for _, ecosystem := range ecosystems {
		for _, marker := range ecosystem.manifests {
			if path.Base(name) == marker {
				return ecosystem.name
			}
		}
	}
	return ""
}

// FileRole reports known static roles; unknown regular files need no invented role.
func FileRole(name string) string {
	switch path.Base(name) {
	case "Makefile", "makefile", "GNUmakefile", "CMakeLists.txt", "build.gradle", "build.gradle.kts":
		return "build_script"
	case "go.sum", "uv.lock", "poetry.lock", "Pipfile.lock", "Cargo.lock", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb":
		return "lockfile"
	}
	return ""
}
