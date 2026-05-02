package cli

import "github.com/htxryan/claude-code-config-profiles/internal/cli/jsonout"

// marshalJSONLine routes through the jsonout package — the single
// deterministic marshaller. Lives in cli/ so output.go can call it without
// importing jsonout directly (avoids the appearance of two encoders).
func marshalJSONLine(payload interface{}) ([]byte, error) {
	return jsonout.MarshalLine(payload)
}
