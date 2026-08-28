package nativesurface

import (
	"os"
	"strings"
	"testing"
)

func TestPreflightJudgesEffectivePnpmAtRepositoryRoot(t *testing.T) {
	sourceBytes, err := os.ReadFile("scripts/check-build-environment.sh")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	if !strings.Contains(source, `pnpm_actual=$(cd "$root" && pnpm --version`) {
		t.Error("preflight does not resolve pnpm at the consumer repository root")
	}
	for _, forbidden := range []string{"pnpm_executable", "realpathSync"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("preflight inspects the pnpm launcher instead of the effective consumer version: %s", forbidden)
		}
	}
}
