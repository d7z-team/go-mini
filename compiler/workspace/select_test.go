package workspace

import "testing"

func TestSelectPackagesRelativeToWorkingDirectory(t *testing.T) {
	sources, err := NewTreeSourceSet("example", []TreeFile{
		{Path: "root.mgo", Text: "package root"},
		{Path: "cmd/tool/main.mgo", Text: "package main"},
		{Path: "cmd/tool/child/value.mgo", Text: "package child"},
		{Path: "lib/value.mgo", Text: "package lib"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	tests := []struct {
		name     string
		workDir  string
		patterns []string
		want     []string
	}{
		{name: "current", workDir: root + "/cmd/tool", want: []string{"example/cmd/tool"}},
		{name: "recursive", workDir: root + "/cmd/tool", patterns: []string{"./..."}, want: []string{"example/cmd/tool", "example/cmd/tool/child"}},
		{name: "relative", workDir: root, patterns: []string{"./lib"}, want: []string{"example/lib"}},
		{name: "import", workDir: root + "/cmd", patterns: []string{"example/lib"}, want: []string{"example/lib"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := SelectPackages(root, test.workDir, "example", sources, test.patterns)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("SelectPackages() = %v, want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("SelectPackages() = %v, want %v", got, test.want)
				}
			}
		})
	}
}
