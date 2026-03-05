package bump

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// Config holds the configuration for version bumping operations.
type Config struct {
	MajorDelta uint64
	MinorDelta uint64
	PatchDelta uint64
	Exact      string // Exact version to set (e.g. "1.2.3"). If set, this takes precedence over the delta fields.
}

// Version calculates the new version based on the provided current version and the bump configuration.
func Version(version string, config *Config) (string, error) {
	if config.Exact != "" {
		ev, err := semver.StrictNewVersion(config.Exact)
		if err != nil {
			return "", fmt.Errorf("invalid version %q: %w", config.Exact, err)
		}
		if v, err := semver.StrictNewVersion(version); err == nil {
			if !ev.GreaterThan(v) {
				return "", fmt.Errorf("version %s is not greater than the current version %s", ev, v)
			}
		}
		return ev.String(), nil
	}

	v, err := semver.StrictNewVersion(version)
	if err != nil {
		return "", fmt.Errorf("invalid current version %q: %w", version, err)
	}

	if config.MajorDelta > 0 {
		for i := uint64(0); i < config.MajorDelta; i++ {
			*v = v.IncMajor()
		}
	} else if config.MinorDelta > 0 {
		for i := uint64(0); i < config.MinorDelta; i++ {
			*v = v.IncMinor()
		}
	} else if config.PatchDelta > 0 {
		for i := uint64(0); i < config.PatchDelta; i++ {
			*v = v.IncPatch()
		}
	}

	return v.String(), nil
}
