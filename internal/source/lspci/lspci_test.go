package lspci

import "testing"

// An empty PCI address short-circuits without touching the cache.
func TestDescriptionEmptyAddress(t *testing.T) {
	if got := Default().Description(""); got != "" {
		t.Errorf("Description(\"\") = %q, want empty", got)
	}
}

// A PCI address that is not in the lspci output (or lspci unavailable)
// yields an empty description, never an error or panic.
func TestDescriptionUnknownAddress(t *testing.T) {
	got := Default().Description("ffff:ff:ff.f")
	if got != "" {
		t.Errorf("Description(unknown) = %q, want empty", got)
	}
}

func TestAvailableDoesNotPanic(t *testing.T) {
	_ = Default().Available()
}
