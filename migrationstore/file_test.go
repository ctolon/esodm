package migrationstore

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ctolon/esodm"
)

func TestFileReplaceAndRecovery(t *testing.T) {
	dir := t.TempDir()
	file := &File{Path: filepath.Join(dir, "migration.json")}
	if _, err := file.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for _, phase := range []string{"copying", "switching", "complete"} {
		state := esodm.MigrationState{Version: 1, Phase: phase, TaskID: "node:1"}
		if err := file.Save(state); err != nil {
			t.Fatal(err)
		}
		restarted := &File{Path: file.Path}
		got, err := restarted.Load()
		if err != nil || got.Phase != phase || got.TaskID != state.TaskID {
			t.Fatal(got, err)
		}
	}
	info, err := os.Stat(file.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	// Encoding failure must preserve the last durable checkpoint.
	if err = file.Save(esodm.MigrationState{Mapping: []byte("invalid")}); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	got, err := file.Load()
	if err != nil || got.Phase != "complete" {
		t.Fatal(got, err)
	}
}

func TestFileFailures(t *testing.T) {
	dir := t.TempDir()
	file := &File{Path: filepath.Join(dir, "checkpoint.json")}
	if err := os.WriteFile(file.Path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Load(); err == nil {
		t.Fatal("corrupt checkpoint accepted")
	}
	oversized := strings.Repeat("x", maxCheckpointBytes+1)
	if err := file.Save(esodm.MigrationState{Phase: oversized}); err == nil {
		t.Fatal("oversized checkpoint saved")
	}
	if err := os.WriteFile(file.Path, []byte(oversized), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Load(); err == nil {
		t.Fatal("oversized checkpoint loaded")
	}
	file.Path = filepath.Join(dir, "missing", "state.json")
	if err := file.Save(esodm.MigrationState{}); err == nil {
		t.Fatal("missing parent accepted")
	}
	file.Path = dir
	if err := file.Save(esodm.MigrationState{}); err == nil {
		t.Fatal("directory replaced")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("temporary file leaked", files, err)
	}
}

func TestFileConcurrentSnapshots(t *testing.T) {
	file := &File{Path: filepath.Join(t.TempDir(), "state.json")}
	if err := file.Save(esodm.MigrationState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			if err := file.Save(esodm.MigrationState{Version: 1, Documents: int64(i)}); err != nil {
				t.Error(err)
			}
			got, err := file.Load()
			if err != nil || got.Version != 1 {
				t.Error(got, err)
			}
		})
	}
	wg.Wait()
}
