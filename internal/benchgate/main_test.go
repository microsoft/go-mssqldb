package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const cleanCSV = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.01e-07,0%,~,p=0.400 n=10
`

const regressedCSV = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,+25.00%,p=0.000 n=10
`

func TestDetectExitCodes(t *testing.T) {
	for _, tt := range []struct {
		name string
		csv  string
		want int
	}{
		{"nothing significant", cleanCSV, exitClean},
		{"a regression", regressedCSV, exitFound},
		{"unrecognised output fails closed", "not benchstat output\n", exitFailure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			code, err := detect([]string{
				"-csv", writeTemp(t, "in.csv", tt.csv),
				"-flagged", filepath.Join(dir, "flagged.tsv"),
				"-selector", filepath.Join(dir, "selector.txt"),
			})
			if tt.want == exitFailure {
				if err == nil {
					t.Fatalf("want an error, got code=%d", code)
				}
				return
			}
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if code != tt.want {
				t.Errorf("code = %d, want %d", code, tt.want)
			}
		})
	}
}

func TestDetectWritesFlaggedAndSelector(t *testing.T) {
	dir := t.TempDir()
	flagged := filepath.Join(dir, "flagged.tsv")
	selector := filepath.Join(dir, "selector.txt")

	code, err := detect([]string{
		"-csv", writeTemp(t, "in.csv", regressedCSV),
		"-flagged", flagged,
		"-selector", selector,
	})
	if err != nil || code != exitFound {
		t.Fatalf("detect: code=%d err=%v", code, err)
	}

	got, err := os.ReadFile(flagged)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Foo-4\tsec/op\n" {
		t.Errorf("flagged = %q", got)
	}
	sel, err := os.ReadFile(selector)
	if err != nil {
		t.Fatal(err)
	}
	if string(sel) != "^Benchmark(Foo)$\n" {
		t.Errorf("selector = %q", sel)
	}
}

func TestDetectMissingFile(t *testing.T) {
	if _, err := detect([]string{"-csv", filepath.Join(t.TempDir(), "absent.csv")}); err == nil {
		t.Fatal("want an error for a missing csv")
	}
}

func TestConfirmExitCodes(t *testing.T) {
	flagged := writeTemp(t, "flagged.tsv", "Foo-4\tsec/op\n")

	code, err := confirm([]string{"-csv", writeTemp(t, "a.csv", regressedCSV), "-flagged", flagged})
	if err != nil || code != exitFound {
		t.Errorf("reproduced: code=%d err=%v, want %d", code, err, exitFound)
	}

	code, err = confirm([]string{"-csv", writeTemp(t, "b.csv", cleanCSV), "-flagged", flagged})
	if err != nil || code != exitClean {
		t.Errorf("not reproduced: code=%d err=%v, want %d", code, err, exitClean)
	}

	// A confirmation that measured nothing must not read as "did not reproduce".
	if _, err := confirm([]string{"-csv", writeTemp(t, "c.csv", "junk\n"), "-flagged", flagged}); err == nil {
		t.Error("want an error when the confirmation produced no comparable rows")
	}

	empty := writeTemp(t, "empty.tsv", "")
	if _, err := confirm([]string{"-csv", writeTemp(t, "d.csv", regressedCSV), "-flagged", empty}); err == nil {
		t.Error("want an error when no rows were flagged")
	}
}

func TestFlaggedRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "flagged.tsv")
	rows := []Row{
		{Name: "Foo/size=1,024-4", Unit: "sec/op"},
		{Name: "Bar-4", Unit: "B/op"},
	}
	if err := writeFlagged(p, rows); err != nil {
		t.Fatal(err)
	}
	got, err := readFlagged(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []Key{{"Foo/size=1,024-4", "sec/op"}, {"Bar-4", "B/op"}}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadFlaggedSkipsMalformedLines(t *testing.T) {
	p := writeTemp(t, "flagged.tsv", "Foo-4\tsec/op\nnotabhere\n\nBar-4\tB/op\n")
	got, err := readFlagged(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2: %+v", len(got), got)
	}
}
