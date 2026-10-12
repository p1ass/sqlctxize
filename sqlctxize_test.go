package sqlctxize

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update .golden files")

func TestRewrite(t *testing.T) {
	tests := []struct {
		dir     string
		exclude []string
	}{
		{dir: "basic"},
		{dir: "selection", exclude: []string{"excluded"}},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			module := t.TempDir()
			if err := os.CopyFS(module, os.DirFS("testdata")); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(module, tt.dir)
			opts := Options{Dir: dir, Exclude: tt.exclude}

			results, err := Rewrite(opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range results {
				if err := os.WriteFile(r.Path, r.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			assertGolden(t, dir, filepath.Join("testdata", tt.dir))

			again, err := Rewrite(opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range again {
				t.Errorf("second run changed %s:\n%s", r.Path, r.Content)
			}

			for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
				cmd := exec.Command("go", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, out)
				}
			}
		})
	}
}

// assertGolden compares each rewritten .go file under got with the .go.golden next to its source under want. A file without .golden must stay unchanged.
func assertGolden(t *testing.T, got, want string) {
	t.Helper()
	err := filepath.WalkDir(want, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		rel, _ := filepath.Rel(want, path)
		actual, err := os.ReadFile(filepath.Join(got, rel))
		if err != nil {
			return err
		}
		golden := path + ".golden"
		if *update {
			original, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Equal(actual, original) {
				os.Remove(golden)
				return nil
			}
			return os.WriteFile(golden, actual, 0o644)
		}
		expected, err := os.ReadFile(golden)
		if os.IsNotExist(err) {
			expected, err = os.ReadFile(path)
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, expected) {
			t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", rel, actual, expected)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTooNewError(t *testing.T) {
	got := tooNewError("example.com/app", "go1.27", "go1.24.3").Error()
	for _, want := range []string{"requires go1.27", "built with go1.24.3", "GOTOOLCHAIN=go1.27.0 go install"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q does not contain %q", got, want)
		}
	}
}
