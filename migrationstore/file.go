// Package migrationstore provides local, atomic storage for migration checkpoints.
package migrationstore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ctolon/esodm"
)

const maxCheckpointBytes = 4 << 20

// File stores one migration checkpoint. Its zero value is not usable; set Path
// before use. Do not copy a File after use or change Path concurrently. Calls on
// one instance are serialized. Applications must serialize other processes and
// instances that share the path, and keep the containing directory trusted.
// Atomic replacement and directory syncing require a filesystem that supports
// them. A Save error after rename may leave the new checkpoint installed.
type File struct {
	// Path is the checkpoint file path. Its parent directory must already exist;
	// use an application-controlled path and serialize writers.
	Path string
	mu   sync.Mutex
}

// Save writes JSON to a private temporary file, syncs it, atomically replaces Path,
// and syncs its parent directory on Unix. Windows does not guarantee directory-entry durability. The parent directory must already exist.
// Save can be assigned directly to MigrationOptions.Checkpoint.
func (f *File) Save(state esodm.MigrationState) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxCheckpointBytes {
		return fmt.Errorf("checkpoint exceeds %d bytes", maxCheckpointBytes)
	}
	dir := filepath.Dir(f.Path)
	// Open the directory before any mutation so access failures preserve the old file.
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	tmp, err := os.CreateTemp(dir, ".esodm-checkpoint-*")
	if err != nil {
		return err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), f.Path); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return parent.Sync()
}

// Load reads at most 4 MiB from Path. A missing file returns an error matching
// os.ErrNotExist. ResumeMigration validates the decoded state against the cluster
// before acting on it.
func (f *File) Load() (esodm.MigrationState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var state esodm.MigrationState
	file, err := os.Open(f.Path)
	if err != nil {
		return state, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxCheckpointBytes+1))
	if err != nil {
		return state, err
	}
	if len(data) > maxCheckpointBytes {
		return state, fmt.Errorf("checkpoint exceeds %d bytes", maxCheckpointBytes)
	}
	err = json.Unmarshal(data, &state)
	return state, err
}
