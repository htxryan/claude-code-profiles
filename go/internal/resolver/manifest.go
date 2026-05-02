package resolver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	pipelineerrors "github.com/htxryan/c3p/internal/errors"
)

var knownManifestFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"extends":     {},
	"includes":    {},
	"tags":        {},
}

// ManifestLoadResult is the loadManifest return: parsed manifest + warnings.
type ManifestLoadResult struct {
	Manifest ProfileManifest
	Warnings []ResolutionWarning
}

// LoadManifest reads and validates profile.json under profileDir. Returns
// the parsed manifest plus any non-fatal warnings (R36 unknown-field
// warnings, missing-manifest warnings).
//
// Behavior matches src/resolver/manifest.ts:
//   - Missing profile.json → empty manifest + MissingManifest warning.
//   - Unparseable JSON → InvalidManifestError.
//   - Wrong types → InvalidManifestError.
//   - Unknown fields → kept out of the returned manifest, surfaced as warnings.
func LoadManifest(profileDir, source string) (ManifestLoadResult, error) {
	manifestPath := filepath.Join(profileDir, "profile.json")
	warnings := []ResolutionWarning{}

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			warnings = append(warnings, ResolutionWarning{
				Code:    WarningMissingManifest,
				Message: fmt.Sprintf("No profile.json at %s; using defaults", manifestPath),
				Source:  source,
			})
			return ManifestLoadResult{Manifest: ProfileManifest{}, Warnings: warnings}, nil
		}
		return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, err.Error())
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(
			manifestPath,
			fmt.Sprintf("JSON parse error: %s", err.Error()),
		)
	}

	obj, ok := parsed.(map[string]any)
	if !ok {
		return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(
			manifestPath,
			"expected a JSON object at the root",
		)
	}

	manifest := ProfileManifest{}

	if v, present := obj["name"]; present {
		s, ok := v.(string)
		if !ok {
			return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"name" must be a string`)
		}
		manifest.Name = s
	}
	if v, present := obj["description"]; present {
		s, ok := v.(string)
		if !ok {
			return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"description" must be a string`)
		}
		manifest.Description = s
	}
	if v, present := obj["extends"]; present {
		s, ok := v.(string)
		if !ok {
			return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"extends" must be a string`)
		}
		manifest.Extends = s
	}
	if v, present := obj["includes"]; present {
		arr, ok := v.([]any)
		if !ok {
			return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"includes" must be an array of strings`)
		}
		out := make([]string, 0, len(arr))
		for _, x := range arr {
			s, ok := x.(string)
			if !ok {
				return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"includes" must be an array of strings`)
			}
			out = append(out, s)
		}
		manifest.Includes = out
	}
	if v, present := obj["tags"]; present {
		arr, ok := v.([]any)
		if !ok {
			return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"tags" must be an array of strings`)
		}
		out := make([]string, 0, len(arr))
		for _, x := range arr {
			s, ok := x.(string)
			if !ok {
				return ManifestLoadResult{}, pipelineerrors.NewInvalidManifestError(manifestPath, `"tags" must be an array of strings`)
			}
			out = append(out, s)
		}
		manifest.Tags = out
	}

	for key := range obj {
		if _, known := knownManifestFields[key]; !known {
			warnings = append(warnings, ResolutionWarning{
				Code:    WarningUnknownManifestField,
				Message: fmt.Sprintf("Unknown field %q in %s", key, manifestPath),
				Source:  source,
			})
		}
	}

	return ManifestLoadResult{Manifest: manifest, Warnings: warnings}, nil
}
