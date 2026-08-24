package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"charm.land/lipgloss/v2"
)

// The mode note carries the impersonated identity, and the help overlay is the
// one place it is meant to be readable in full. A note wider than the terminal
// would set the box width and lose both the right border and the identity to
// the overlay clamp.
func TestHelpViewWrapsLongModeNote(t *testing.T) {
	h := newHelpView(PickTheme("ansi"), defaultKeys())
	h.note = "Impersonating system:serviceaccount:kube-system:cluster-admin (groups: system:masters) · Developer + read-only mode: nav scoped to app resources; all writes disabled"

	for _, width := range []int{60, 80, 100, 132} {
		got := ansi.Strip(h.View(width, 30))
		if w := lipgloss.Width(got); w > width {
			t.Errorf("width %d: View() rendered %d columns wide", width, w)
		}
		if !strings.Contains(got, "system:serviceaccount:kube-system:cluster-admin") {
			t.Errorf("width %d: View() = %q, want the full impersonated identity", width, got)
		}
		if !strings.Contains(got, "all writes disabled") {
			t.Errorf("width %d: View() lost the tail of the mode note", width)
		}
	}
}

// The note takes rows away from the keybinding grid, so a wrapped note has to
// shrink the grid by its own line count or the box outgrows the overlay.
func TestHelpViewNoteHeightAccountsForWrapping(t *testing.T) {
	th := PickTheme("ansi")
	keys := defaultKeys()
	plain := newHelpView(th, keys)
	wrapped := newHelpView(th, keys)
	wrapped.note = "Impersonating system:admin (groups: system:masters) · Developer + read-only mode: nav scoped to app resources; all writes disabled"

	const width, height = 80, 30
	plainRows := strings.Count(plain.View(width, height), "\n")
	wrappedRows := strings.Count(wrapped.View(width, height), "\n")
	if wrappedRows != plainRows {
		t.Fatalf("note changed the box height: %d rows with a note, %d without", wrappedRows, plainRows)
	}
}
