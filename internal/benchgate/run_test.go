package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDispatch(t *testing.T) {
	csv := writeTemp(t, "in.csv", cleanCSV)
	flagged := writeTemp(t, "flagged.tsv", "Foo-4\tsec/op\n")

	for _, tt := range []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, exitFailure},
		{"unknown subcommand", []string{"explode"}, exitFailure},
		{"detect with nothing significant", []string{"detect", "-csv", csv}, exitClean},
		{"detect surfacing an error", []string{"detect", "-csv", "no-such-file.csv"}, exitFailure},
		{"confirm not reproduced", []string{"confirm", "-csv", csv, "-flagged", flagged}, exitClean},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			if got := run(tt.args, &out); got != tt.want {
				t.Errorf("run(%v) = %d, want %d (output: %s)", tt.args, got, tt.want, out.String())
			}
		})
	}
}

func TestRunReportsUsageAndErrors(t *testing.T) {
	var out strings.Builder
	run(nil, &out)
	if !strings.Contains(out.String(), "usage:") {
		t.Errorf("no usage line: %q", out.String())
	}

	out.Reset()
	run([]string{"explode"}, &out)
	if !strings.Contains(out.String(), "::error::") {
		t.Errorf("no error annotation: %q", out.String())
	}
}

func TestDetectRejectsUnknownFlag(t *testing.T) {
	if _, err := detect([]string{"-nosuchflag"}); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}

func TestConfirmRejectsUnknownFlag(t *testing.T) {
	if _, err := confirm([]string{"-nosuchflag"}); err == nil {
		t.Fatal("want an error for an unknown flag")
	}
}

func TestConfirmMissingFiles(t *testing.T) {
	csv := writeTemp(t, "in.csv", regressedCSV)
	flagged := writeTemp(t, "flagged.tsv", "Foo-4\tsec/op\n")

	if _, err := confirm([]string{"-csv", filepath.Join(t.TempDir(), "absent.csv"), "-flagged", flagged}); err == nil {
		t.Error("want an error for a missing csv file")
	}
	if _, err := confirm([]string{"-csv", csv, "-flagged", filepath.Join(t.TempDir(), "absent.tsv")}); err == nil {
		t.Error("want an error for a missing flagged file")
	}
}

// A CSV containing improvements exercises the reporting path.
func TestDetectReportsImprovements(t *testing.T) {
	const improved = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1.25e-07,0%,1e-07,0%,-20.00%,p=0.000 n=10
`
	code, err := detect([]string{"-csv", writeTemp(t, "in.csv", improved)})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if code != exitClean {
		t.Errorf("code = %d, want %d", code, exitClean)
	}
}

// Writing to a path that is a directory fails, which covers the write paths.
func TestDetectSurfacesWriteErrors(t *testing.T) {
	dir := t.TempDir()
	csv := writeTemp(t, "in.csv", regressedCSV)

	if _, err := detect([]string{"-csv", csv, "-flagged", dir}); err == nil {
		t.Error("want an error when the flagged path cannot be written")
	}
	if _, err := detect([]string{"-csv", csv, "-flagged", filepath.Join(dir, "ok.tsv"), "-selector", dir}); err == nil {
		t.Error("want an error when the selector path cannot be written")
	}
}

func TestReadFlaggedMissingFile(t *testing.T) {
	if _, err := readFlagged(filepath.Join(t.TempDir(), "absent.tsv")); err == nil {
		t.Fatal("want an error for a missing file")
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestParseSurfacesCSVErrors(t *testing.T) {
	_, err := Parse(failingReader{err: errors.New("disk fell over")})
	if err == nil {
		t.Fatal("want an error when the reader fails")
	}
	if !strings.Contains(err.Error(), "read csv") {
		t.Errorf("error = %v, want it wrapped with 'read csv'", err)
	}
	if !strings.Contains(err.Error(), "disk fell over") {
		t.Errorf("error = %v, want the underlying cause preserved", err)
	}
}

// A delta the shape check accepts but ParseFloat cannot represent means our own
// assumption is broken, so it must fail closed rather than drop the row.
func TestParseFailsClosedOnOutOfRangeDelta(t *testing.T) {
	huge := "+" + strings.Repeat("9", 400) + ".00%"
	in := ",old,,new,,,\n,sec/op,CI,sec/op,CI,vs base,P\nFoo-4,1e-07,0%,1e-07,0%," + huge + ",p=0.000 n=10\n"
	_, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatal("want an error for a delta that cannot be represented")
	}
	if !strings.Contains(err.Error(), "Foo-4") {
		t.Errorf("error = %v, want it to name the offending row", err)
	}
}

func TestWriteFlaggedSurfacesErrors(t *testing.T) {
	if err := writeFlagged(t.TempDir(), []Row{{Name: "Foo-4", Unit: "sec/op"}}); err == nil {
		t.Fatal("want an error writing to a directory")
	}
}

func TestRunReturnsFoundOnRegression(t *testing.T) {
	var out strings.Builder
	if got := run([]string{"detect", "-csv", writeTemp(t, "x.csv", regressedCSV)}, &out); got != exitFound {
		t.Errorf("run detect on a regression = %d, want %d", got, exitFound)
	}
}
