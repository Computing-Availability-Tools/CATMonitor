package main

import "testing"

func TestStressHelpRequested(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"run", "-h"}} {
		if !stressHelpRequested(args) {
			t.Fatalf("stressHelpRequested(%v) = false, want true", args)
		}
	}
	if stressHelpRequested([]string{"run", "--bench", "stream"}) {
		t.Fatal("normal stress command was treated as help")
	}
}

func TestStressArgs(t *testing.T) {
	configPath, names, output, err := stressArgs([]string{"run", "--bench", "stream,hpl", "-c", "/tmp/catmonitor.yaml", "-o", "table"})
	if err != nil {
		t.Fatal(err)
	}
	if configPath != "/tmp/catmonitor.yaml" || output != "table" || len(names) != 2 || names[0] != "stream" || names[1] != "hpl" {
		t.Fatalf("unexpected arguments: config=%q names=%v output=%q", configPath, names, output)
	}
}
