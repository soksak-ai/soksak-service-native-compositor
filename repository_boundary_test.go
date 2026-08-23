package nativesurface

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryIgnoresTheDeclaredPackageOutput(t *testing.T) {
	body, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	found := false
	for _, line := range lines {
		if line == "node_modules/" {
			found = true
		}
	}
	if !found {
		t.Fatal("root package output node_modules/ is not ignored")
	}
}

func TestRepositoryApprovesItsBuildDependency(t *testing.T) {
	body, err := os.ReadFile("pnpm-workspace.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "allowBuilds:\n  esbuild: true\n" {
		t.Fatalf("pnpm build approval = %q", body)
	}
}
