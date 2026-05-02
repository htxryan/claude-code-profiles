package commands

import (
	"github.com/htxryan/claude-code-config-profiles/internal/cli/jsonout"
	"github.com/htxryan/claude-code-config-profiles/internal/resolver"
)

// marshalManifest serialises a ProfileManifest to JSON via the central
// jsonout package so the lint gate (forbidding json.Marshal outside
// jsonout/) stays clean. Includes a trailing newline for tool friendliness.
func marshalManifest(m resolver.ProfileManifest) ([]byte, error) {
	return jsonout.Marshal(m)
}
