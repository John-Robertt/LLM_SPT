//go:build windows

package filesystem

import (
	"testing"

	"llmspt/pkg/contract"
)

// TestMapPathInvalidWindows Windows-specific path validation
func TestMapPathInvalidWindows(t *testing.T) {
	dir := t.TempDir()
	flat := false
	w, _ := New(&Options{OutputDir: dir, Flat: &flat})
	cases := []string{"C:\\abs", "..", "."} // C:\abs is absolute on Windows
	for _, id := range cases {
		if _, err := w.mapPath(contract.ArtifactID(id)); err != contract.ErrPathInvalid {
			t.Fatalf("id %s expect invalid", id)
		}
	}
}