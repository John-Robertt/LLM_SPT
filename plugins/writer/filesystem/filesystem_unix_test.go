//go:build !windows

package filesystem

import (
	"testing"

	"llmspt/pkg/contract"
)

// TestMapPathInvalidUnix Unix-specific path validation
func TestMapPathInvalidUnix(t *testing.T) {
	dir := t.TempDir()
	flat := false
	w, _ := New(&Options{OutputDir: dir, Flat: &flat})
	cases := []string{"/abs", "..", "."} // /abs is absolute on Unix
	for _, id := range cases {
		if _, err := w.mapPath(contract.ArtifactID(id)); err != contract.ErrPathInvalid {
			t.Fatalf("id %s expect invalid", id)
		}
	}
}