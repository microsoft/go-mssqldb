package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDispatch(t *testing.T) {
	csv := writeTemp(t, "in.csv", cleanCSV)
	flagged := writeTemp(t, "flagged.tsv", "example.com/root\tFoo-4\tsec/op\n")

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
	flagged := writeTemp(t, "flagged.tsv", "example.com/root\tFoo-4\tsec/op\n")

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

// The selector addresses a flagged sub-benchmark through its parent, so the
// recheck can come back full of siblings while the candidate itself never ran.
// Non-empty output must not be read as "the candidate came back clean".
func TestConfirmFailsWhenCandidateWasNotRemeasured(t *testing.T) {
	const siblingsOnly = `pkg: example.com/root
,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Parent/Other-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
`
	csv := writeTemp(t, "recheck.csv", siblingsOnly)
	flagged := writeTemp(t, "flagged.tsv", "example.com/root\tParent/Sub-4\tsec/op\n")

	code, err := confirm([]string{"-csv", csv, "-flagged", flagged})
	if err == nil {
		t.Fatal("an unmeasured candidate was accepted as runner noise")
	}
	if code != exitFailure {
		t.Errorf("code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(err.Error(), "Parent/Sub-4") {
		t.Errorf("error = %v, want it to name the unmeasured candidate", err)
	}
}

// A candidate that was remeasured and came back clean is still noise.
func TestConfirmAcceptsNoiseOnlyWhenCandidateWasRemeasured(t *testing.T) {
	const remeasured = `pkg: example.com/root
,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Parent/Sub-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
Parent/Other-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
`
	csv := writeTemp(t, "recheck.csv", remeasured)
	flagged := writeTemp(t, "flagged.tsv", "example.com/root\tParent/Sub-4\tsec/op\n")

	code, err := confirm([]string{"-csv", csv, "-flagged", flagged})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if code != exitClean {
		t.Errorf("code = %d, want %d", code, exitClean)
	}
}

func TestUnmeasured(t *testing.T) {
	rows := []Row{
		{Name: "Foo-4", Unit: "sec/op"},
		{Name: "Foo-4", Unit: "B/s"},
	}
	for _, tt := range []struct {
		name    string
		flagged []Key
		want    int
	}{
		{"all present", []Key{{Name: "Foo-4", Unit: "sec/op"}}, 0},
		{"name absent", []Key{{Name: "Bar-4", Unit: "sec/op"}}, 1},
		{"same name, different unit", []Key{{Name: "Foo-4", Unit: "allocs/op"}}, 1},
		{"same name and unit, different package", []Key{{Package: "example.com/other", Name: "Foo-4", Unit: "sec/op"}}, 1},
		{"mixed", []Key{{Name: "Foo-4", Unit: "sec/op"}, {Name: "Bar-4", Unit: "sec/op"}}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Unmeasured(rows, tt.flagged); len(got) != tt.want {
				t.Errorf("Unmeasured() = %+v, want %d entries", got, tt.want)
			}
		})
	}
}

// An unlisted unit has no direction, so a significant change in it cannot be
// classified. Guessing inverts it: errors/op rising would read as an
// improvement and errors/op falling would fail the build.
func TestParseFailsClosedOnUnknownUnitWithSignificantDelta(t *testing.T) {
	const in = `,old,,new,,,
,errors/op,CI,errors/op,CI,vs base,P
Foo-4,1,0%,1.25,0%,+25.00%,p=0.000 n=10
`
	_, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatal("want an error for a significant delta in a unit with no known direction")
	}
	if !strings.Contains(err.Error(), "errors/op") {
		t.Errorf("error = %v, want it to name the unit", err)
	}
}

// An unknown unit that did not move cannot be misclassified, so it must not
// fail the build: benchmarks report descriptive metrics like msgs/op that are
// stable and irrelevant to the gate.
func TestParseAllowsUnknownUnitWithoutSignificantDelta(t *testing.T) {
	const in = `,old,,new,,,
,msgs/op,CI,msgs/op,CI,vs base,P
Foo-4,10,0%,10,0%,~,p=0.900 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 || rows[0].Significant {
		t.Fatalf("got %+v, want one insignificant row", rows)
	}
}

func TestUnitDirectionCoversBenchstatUnits(t *testing.T) {
	for _, u := range []string{"sec/op", "ns/op", "B/op", "allocs/op"} {
		if !lowerIsBetter(u) {
			t.Errorf("%s should be lower-is-better", u)
		}
	}
	for _, u := range []string{"B/s", "MB/s"} {
		if lowerIsBetter(u) {
			t.Errorf("%s should be higher-is-better", u)
		}
	}
}

// LazyQuotes would turn an unterminated quote into a silently truncated record
// instead of an error, and the short-row skip would then drop the regression
// while the clean row kept the result non-empty.
func TestParseFailsClosedOnUnterminatedQuote(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Clean-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
"Regressed/size=1,024-4,1e-07,0%,1.25e-07,0%,+25.00%,p=0.000 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatalf("malformed quoting was accepted: %+v", rows)
	}
}

// A short row is only legitimate when one side was not measured. Both sides
// present with no delta column is drift, and skipping it would hide whatever
// the comparison would have said.
func TestParseFailsClosedOnShortRowWithBothMeasurements(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Clean-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
Truncated-4,1e-07,0%,1.25e-07,0%
`
	rows, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatalf("a truncated comparison row was skipped: %+v", rows)
	}
	if !strings.Contains(err.Error(), "Truncated-4") {
		t.Errorf("error = %v, want it to name the offending row", err)
	}
}

// Same rule at full width: a blank delta is only acceptable when a side is
// missing.
func TestParseFailsClosedOnBlankDeltaWithBothMeasurements(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Both-4,1e-07,0%,1.25e-07,0%,,
`
	_, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatal("want an error for a blank delta with both measurements present")
	}
	if !strings.Contains(err.Error(), "Both-4") {
		t.Errorf("error = %v, want it to name the offending row", err)
	}
}

// A drifted header must not be mistaken for the file-list line that separates
// tables. Treating it as a separator resets the unit and silently skips the
// whole table, which a preceding intact table would then mask.
func TestParseFailsClosedOnDriftedHeaderAfterGoodTable(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10

,old,,new,,,
,B/op,Confidence,B/op,Confidence,vs base,P
Foo-4,100,0%,125,0%,+25.00%,p=0.000 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatalf("a drifted header was skipped behind an intact table: %+v", rows)
	}
}

// A data row after a separator but with no header of its own has no unit.
// Skipping it drops whatever it says behind the tables that did parse, which is
// the same masking one level coarser than a drifted header.
func TestParseFailsClosedOnRowOutsideAnyTable(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10

,old,,new,,,
Bar-4,100,0%,125,0%,+25.00%,p=0.000 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatalf("a row outside any table was skipped: %+v", rows)
	}
	if !strings.Contains(err.Error(), "Bar-4") {
		t.Errorf("error = %v, want it to name the offending record", err)
	}
}

// The goos/goarch/pkg preamble also arrives before any header, and must keep
// parsing rather than being mistaken for an orphaned row.
func TestParseSkipsPreambleBeforeFirstTable(t *testing.T) {
	const in = `goos: windows
goarch: amd64
pkg: github.com/microsoft/go-mssqldb
,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %+v, want the single Foo-4 row", rows)
	}
}

// The width of an orphaned record does not matter. benchstat's preamble is
// single-column, so anything wider with no table in scope is drift, including a
// short row that would otherwise look like a one-sided benchmark.
func TestParseFailsClosedOnNarrowRowOutsideAnyTable(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10

,old,,new,,,
Bar-4,100,0%,125,0%
`
	rows, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatalf("a narrow row outside any table was skipped: %+v", rows)
	}
	if !strings.Contains(err.Error(), "Bar-4") {
		t.Errorf("error = %v, want it to name the offending record", err)
	}
}

func TestRunReturnsFoundOnRegression(t *testing.T) {
	var out strings.Builder
	if got := run([]string{"detect", "-csv", writeTemp(t, "x.csv", regressedCSV)}, &out); got != exitFound {
		t.Errorf("run detect on a regression = %d, want %d", got, exitFound)
	}
}
