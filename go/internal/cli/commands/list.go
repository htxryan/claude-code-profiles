package commands

import (
	"fmt"

	"github.com/htxryan/c3p/internal/resolver"
	"github.com/htxryan/c3p/internal/state"
)

// listEntryPayload is the per-row JSON shape emitted under --json.
type listEntryPayload struct {
	Name             string   `json:"name"`
	Active           bool     `json:"active"`
	Description      string   `json:"description"`
	Extends          string   `json:"extends"`
	Includes         []string `json:"includes"`
	Tags             []string `json:"tags"`
	LastMaterialized string   `json:"lastMaterialized"`
}

type listPayload struct {
	Profiles     []listEntryPayload `json:"profiles"`
	StateWarning string             `json:"stateWarning"`
}

// RunList implements the `list` verb. Read-only (R43, no lock).
// Mirrors src/cli/commands/list.ts.
func RunList(opts ListOptions) (int, error) {
	paths := state.BuildStatePaths(opts.Cwd)
	st, err := state.ReadStateFile(paths)
	if err != nil {
		return 2, err
	}
	activeName := ""
	if st.State.ActiveProfile != nil {
		activeName = *st.State.ActiveProfile
	}
	lastMaterialized := ""
	if st.State.MaterializedAt != nil {
		lastMaterialized = *st.State.MaterializedAt
	}

	names, err := resolver.ListProfiles(resolver.DiscoverOptions{ProjectRoot: opts.Cwd})
	if err != nil {
		return 2, err
	}

	entries := make([]listEntryPayload, 0, len(names))
	for _, name := range names {
		mlr, _ := resolver.LoadManifest(profileDir(opts.Cwd, name), name)
		manifest := mlr.Manifest
		entry := listEntryPayload{
			Name:        name,
			Active:      name == activeName,
			Description: manifest.Description,
			Extends:     manifest.Extends,
			Includes:    manifest.Includes,
			Tags:        manifest.Tags,
		}
		if entry.Includes == nil {
			entry.Includes = []string{}
		}
		if entry.Tags == nil {
			entry.Tags = []string{}
		}
		if entry.Active {
			entry.LastMaterialized = lastMaterialized
		}
		entries = append(entries, entry)
	}

	stateWarning := ""
	if st.Warning != nil {
		stateWarning = string(st.Warning.Code)
		if st.Warning.Detail != "" {
			stateWarning = stateWarning + ": " + st.Warning.Detail
		}
	}

	if opts.Output.JSONMode() {
		opts.Output.JSON(listPayload{Profiles: entries, StateWarning: stateWarning})
		return 0, nil
	}

	if len(entries) == 0 {
		opts.Output.Print("(no profiles found in .claude-profiles/)")
		return 0, nil
	}

	hasDescription, hasTags := false, false
	for _, e := range entries {
		if e.Description != "" {
			hasDescription = true
		}
		if len(e.Tags) > 0 {
			hasTags = true
		}
	}

	rows := [][]string{}
	header := []string{"NAME"}
	if hasDescription {
		header = append(header, "DESCRIPTION")
	}
	if hasTags {
		header = append(header, "TAGS")
	}
	header = append(header, "META")
	rows = append(rows, header)
	for _, e := range entries {
		nameCell := e.Name
		if e.Active {
			nameCell = "* " + e.Name
		} else {
			nameCell = "  " + e.Name
		}
		row := []string{nameCell}
		if hasDescription {
			row = append(row, e.Description)
		}
		if hasTags {
			row = append(row, joinTags(e.Tags))
		}
		row = append(row, formatListMeta(e))
		rows = append(rows, row)
	}
	// Use the package-internal renderTable from cli — but commands can't
	// import cli. We render inline here.
	opts.Output.Print(renderTable(rows))
	if stateWarning != "" {
		opts.Output.Warn(fmt.Sprintf("note: state file degraded — %s", stateWarning))
	}
	return 0, nil
}

func formatListMeta(e listEntryPayload) string {
	parts := []string{}
	if e.Extends != "" {
		parts = append(parts, "extends "+e.Extends)
	}
	if len(e.Includes) > 0 {
		parts = append(parts, fmt.Sprintf("includes %d", len(e.Includes)))
	}
	if e.Active && e.LastMaterialized != "" {
		parts = append(parts, "last "+e.LastMaterialized)
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " · " + p
	}
	return out
}

func joinTags(tags []string) string {
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += ","
		}
		out += t
	}
	return out
}
