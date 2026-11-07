package cli

import "testing"

func TestCellWidth(t *testing.T) {
	if got := cellWidth("ollama"); got != 6 {
		t.Fatalf("ascii width = %d, want 6", got)
	}
	if got := cellWidth("ＡＢ"); got != 4 {
		t.Fatalf("wide width = %d, want 4", got)
	}
}
