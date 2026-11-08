package integration

import _ "embed"

// pinnedVersions is the shared source for actual container images and generated support documentation.
//
//go:embed versions.json
var pinnedVersions []byte

// engineVersion binds a human-readable version to an immutable container image.
type engineVersion struct {
	Version string `json:"version"`
	Image   string `json:"image"`
}
