package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	clierr "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// DefaultPath returns ".cloudru-vm/state.json" relative to the given project root.
func DefaultPath(root string) string {
	return filepath.Join(root, ".cloudru-vm", "state.json")
}

// Read reads the state file. Returns empty File (version=1) if file doesn't exist.
// Returns CLIError(STATE_CORRUPTED) if JSON is invalid.
func Read(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{
				Version:     1,
				Deployments: []DeploymentRecord{},
			}, nil
		}
		return nil, clierr.Wrap(
			clierr.CodeStateCorrupted,
			"failed to read state file",
			"Check file permissions for "+path,
			err,
		)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, clierr.Wrap(
			clierr.CodeStateCorrupted,
			"state file contains invalid JSON",
			"Delete "+path+" and retry, or fix the JSON manually",
			err,
		)
	}

	if f.Deployments == nil {
		f.Deployments = []DeploymentRecord{}
	}

	return &f, nil
}

// Write atomically writes the state file (write .tmp, rename).
// Creates parent directory if needed.
func Write(path string, f *File) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			"failed to create state directory",
			"Check write permissions for "+dir,
			err,
		)
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			"failed to marshal state",
			"This is a bug; please report it",
			err,
		)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			"failed to write temporary state file",
			"Check disk space and permissions for "+dir,
			err,
		)
	}

	if err := os.Rename(tmp, path); err != nil {
		// Clean up tmp file on rename failure.
		_ = os.Remove(tmp)
		return clierr.Wrap(
			clierr.CodeStateCorrupted,
			"failed to atomically replace state file",
			"Check permissions for "+path,
			err,
		)
	}

	return nil
}

// FindDeployment returns the first deployment with the given ID, or nil.
func (f *File) FindDeployment(id string) *DeploymentRecord {
	for i := range f.Deployments {
		if f.Deployments[i].ID == id {
			return &f.Deployments[i]
		}
	}
	return nil
}

// ActiveDeployment returns the first deployment with status != "destroyed", or nil.
func (f *File) ActiveDeployment() *DeploymentRecord {
	for i := range f.Deployments {
		if f.Deployments[i].Status != "destroyed" {
			return &f.Deployments[i]
		}
	}
	return nil
}

// UpsertDeployment adds or updates a deployment record by ID.
func (f *File) UpsertDeployment(rec DeploymentRecord) {
	for i := range f.Deployments {
		if f.Deployments[i].ID == rec.ID {
			f.Deployments[i] = rec
			return
		}
	}
	f.Deployments = append(f.Deployments, rec)
}

// RemoveDeployment removes a deployment by ID.
func (f *File) RemoveDeployment(id string) {
	n := 0
	for _, d := range f.Deployments {
		if d.ID != id {
			f.Deployments[n] = d
			n++
		}
	}
	f.Deployments = f.Deployments[:n]
}
