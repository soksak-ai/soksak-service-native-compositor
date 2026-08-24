package nativesurface

import (
	"os"
	"strings"
	"testing"
)

func TestMakeOwnsCompositorBuildCommands(t *testing.T) {
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	source := string(makefile)
	for _, target := range []string{"preflight:", "prepare:", "build:", "verify:"} {
		if !strings.Contains(source, target) {
			t.Errorf("Makefile omits %s", target)
		}
	}
	manifest, err := os.ReadFile("package.json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(manifest)
	if !strings.Contains(text, `"engines"`) || !strings.Contains(text, `"packageManager"`) {
		t.Error("package.json does not own exact Node and pnpm versions")
	}
	if _, err := os.Stat(".node-version"); err != nil {
		t.Error(err)
	}
}
