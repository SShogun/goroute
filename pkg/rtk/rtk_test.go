package rtk

import (
	"strings"
	"testing"
)

func TestRTKGitStatusFilter(t *testing.T) {
	engine := GlobalEngine
	rawOutput := `On branch main
Your branch is up to date with 'origin/main'.

Changes not staged for commit:
  (use "git add <file>..." to update what will be committed)
  (use "git restore <file>..." to discard changes in working directory)
	modified:   pkg/proxy/server.go
	modified:   pkg/providers/kiro/client.go

Untracked files:
  (use "git add <file>..." to include in what will be committed)
	pkg/affinity/affinity.go
	pkg/rtk/rtk.go

no changes added to commit (use "git add" to track)
`
	res := engine.FilterCommandOutput("git status", rawOutput)
	if res.LinesSaved == 0 {
		t.Logf("Result: matched=%s, orig=%d, filtered=%d", res.MatchedFilterID, res.OriginalLines, res.FilteredLines)
	}
	if !strings.Contains(res.Output, "modified:   pkg/proxy/server.go") {
		t.Fatalf("expected modified file line to be preserved")
	}
}

func TestRTKNpmInstallFilter(t *testing.T) {
	engine := GlobalEngine
	rawOutput := strings.Repeat("npm notice created a lockfile\n", 40) + "added 450 packages in 3s\n3 packages are looking for funding\n"
	res := engine.FilterCommandOutput("npm install", rawOutput)
	if res.LinesSaved <= 0 {
		t.Fatalf("expected npm install noise to be reduced, saved: %d", res.LinesSaved)
	}
}
