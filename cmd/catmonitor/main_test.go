package main

import "testing"

func TestRunStressCommandExitCodes(t *testing.T) {
	if code := runStress(nil); code != 0 {
		t.Fatalf("empty stress command code=%d want 0", code)
	}
	if code := runStress([]string{"--help"}); code != 0 {
		t.Fatalf("help code=%d want 0", code)
	}
	if code := runStress([]string{"bad-subcommand"}); code != 2 {
		t.Fatalf("bad subcommand code=%d want 2", code)
	}
	if code := runStress([]string{"run", "--bad-option"}); code != 2 {
		t.Fatalf("bad option code=%d want 2", code)
	}
}
