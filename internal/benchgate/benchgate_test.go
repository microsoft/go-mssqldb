package main

import (
	"strings"
	"testing"
)

// Shapes below are real `benchstat -format=csv` output, including the header
// lines it prints before each table.
const secOpTable = `goos: linux
goarch: amd64
pkg: github.com/microsoft/go-mssqldb
cpu: Test
,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,+25.00%,p=0.000 n=10
Bar-4,1e-07,0%,1.01e-07,0%,~,p=0.400 n=10
geomean,1e-07,,1.12e-07,,+12.00%,
`

func parseString(t *testing.T, s string) []Row {
	t.Helper()
	rows, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return rows
}

func TestParseReadsNameUnitAndDelta(t *testing.T) {
	rows := parseString(t, secOpTable)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (geomean must be excluded): %+v", len(rows), rows)
	}
	if rows[0] != (Row{Package: "github.com/microsoft/go-mssqldb", Name: "Foo-4", Unit: "sec/op", Delta: 25, Significant: true}) {
		t.Errorf("first row = %+v", rows[0])
	}
	if rows[1].Significant {
		t.Errorf("~ must not be significant: %+v", rows[1])
	}
}

// geomean carries a percentage but is a summary. Selecting it with -bench
// matches no benchmark, so it must never reach the flagged set.
func TestParseExcludesGeomean(t *testing.T) {
	for _, r := range parseString(t, secOpTable) {
		if r.Name == "geomean" {
			t.Fatal("geomean was parsed as a benchmark row")
		}
	}
}

// benchstat quotes names containing commas. Splitting on commas shifts every
// field and silently drops the row.
func TestParseHandlesQuotedNameContainingComma(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
"Foo/size=1,024-4",1e-07,0%,1.25e-07,0%,+25.00%,p=0.000 n=10
`
	rows := parseString(t, in)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].Name != "Foo/size=1,024-4" {
		t.Errorf("name = %q", rows[0].Name)
	}
	if got := Regressions(rows); len(got) != 1 {
		t.Errorf("regression on a comma-named benchmark was dropped")
	}
}

// If benchstat renames a header column the rows are no longer interpretable.
// Erroring keeps the caller from reporting "clean" off a table it cannot read.
func TestParseFailsClosedOnHeaderDrift(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,Confidence,sec/op,Confidence,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,+22.00%,p=0.000 n=10
`
	if _, err := Parse(strings.NewReader(in)); err == nil {
		t.Fatal("want an error for a drifted header")
	}
}

// An unreadable delta must be an error, not a dropped row: dropping it would
// let a regression hide behind the rows that did parse.
func TestParseFailsClosedOnUnreadableDelta(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,20 percent worse,p=0.000 n=10
`
	if _, err := Parse(strings.NewReader(in)); err == nil {
		t.Fatal("want an error for an unreadable delta")
	}
}

// The dangerous shape: one row parses clean and the regression is unreadable.
// A row count alone would look healthy, so this must fail the whole parse.
func TestParseFailsClosedOnMixedValidAndUnreadableRows(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Clean-4,1e-07,0%,1e-07,0%,~,p=0.900 n=10
Regressed-4,1e-07,0%,1.25e-07,0%,25 percent worse,p=0.000 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatalf("unreadable regression was hidden behind a clean row: %+v", rows)
	}
	if !strings.Contains(err.Error(), "Regressed-4") {
		t.Errorf("error = %v, want it to name the offending row", err)
	}
}

// Benchmarks present on only one side have no delta to read. benchstat emits
// them as short rows, and they must not be mistaken for format drift.
func TestParseSkipsOneSidedBenchmarks(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Removed-4,1e-06,∞
Added-4,,,1.5e-06,∞
Both-4,2e-06,0%,2e-06,0%,~,p=0.900 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Both-4" {
		t.Fatalf("got %+v, want only Both-4", rows)
	}
}

// Today benchstat truncates one-sided rows, so this full-width shape does not
// occur. A blank delta still means "no comparison", which is a skip and not the
// format drift that must fail the parse.
func TestParseSkipsFullWidthRowWithBlankDelta(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Added-4,,,1.5e-06,∞,,
Both-4,2e-06,0%,2e-06,0%,~,p=0.900 n=10
`
	rows, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Both-4" {
		t.Fatalf("got %+v, want only Both-4", rows)
	}
}

// A unit must not carry over into the next table.
func TestParseResetsUnitBetweenTables(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.01e-07,0%,~,p=0.400 n=10

,old,,new,,,
,B/s,CI,B/s,CI,vs base,P
Bar-4,5e+07,0%,6.25e+07,0%,+25.00%,p=0.000 n=10
`
	rows := parseString(t, in)
	if len(rows) != 2 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	if rows[1].Unit != "B/s" {
		t.Errorf("second row unit = %q, want B/s", rows[1].Unit)
	}
}

func TestParseTracksPackagePreamble(t *testing.T) {
	const in = `pkg: example.com/root
,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,+25.00%,p=0.000 n=10

pkg: example.com/root/msdsn
,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,+25.00%,p=0.000 n=10
`
	rows := parseString(t, in)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].Package != "example.com/root" || rows[1].Package != "example.com/root/msdsn" {
		t.Errorf("packages = %q, %q", rows[0].Package, rows[1].Package)
	}
}

// The mirror of the gain case: throughput falling is a regression even though
// the delta is negative, so the threshold applies in the opposite direction.
func TestRegressionsCatchThroughputLosses(t *testing.T) {
	rows := []Row{
		{Name: "Foo-4", Unit: "B/s", Delta: -20, Significant: true},
	}
	got := Regressions(rows)
	if len(got) != 1 {
		t.Fatalf("a 20%% throughput loss was not flagged: %+v", got)
	}
	if got[0].Unit != "B/s" {
		t.Errorf("unit = %q, want B/s", got[0].Unit)
	}
	if imp := Improvements(rows); len(imp) != 0 {
		t.Errorf("a throughput loss was reported as an improvement: %+v", imp)
	}
}

func TestRegressionsIgnoreSmallThroughputLosses(t *testing.T) {
	rows := []Row{{Name: "Foo-4", Unit: "B/s", Delta: -14.9, Significant: true}}
	if got := Regressions(rows); len(got) != 0 {
		t.Fatalf("a sub-threshold throughput loss was flagged: %+v", got)
	}
}

// The gated benchmarks call SetBytes, so a speedup shows as -20% sec/op and
// +25% B/s. Treating any positive delta as worse fails the build on a win.
func TestRegressionsIgnoreThroughputGains(t *testing.T) {
	rows := []Row{
		{Name: "Foo-4", Unit: "sec/op", Delta: -20, Significant: true},
		{Name: "Foo-4", Unit: "B/s", Delta: +25, Significant: true},
	}
	if got := Regressions(rows); len(got) != 0 {
		t.Fatalf("a 20%% speedup was reported as a regression: %+v", got)
	}
	if got := Improvements(rows); len(got) != 2 {
		t.Errorf("want both rows reported as improvements, got %+v", got)
	}
}

func TestRegressionsThreshold(t *testing.T) {
	for _, tt := range []struct {
		name  string
		row   Row
		wants bool
	}{
		{"above threshold", Row{Name: "Foo-4", Unit: "sec/op", Delta: 25, Significant: true}, true},
		{"exactly at threshold", Row{Name: "Foo-4", Unit: "sec/op", Delta: 15, Significant: true}, true},
		{"below threshold", Row{Name: "Foo-4", Unit: "sec/op", Delta: 14.99, Significant: true}, false},
		{"not significant", Row{Name: "Foo-4", Unit: "sec/op", Delta: 25}, false},
		{"allocs count", Row{Name: "Foo-4", Unit: "allocs/op", Delta: 25, Significant: true}, true},
		{"bytes per op", Row{Name: "Foo-4", Unit: "B/op", Delta: 25, Significant: true}, true},
		{"throughput", Row{Name: "Foo-4", Unit: "B/s", Delta: 25, Significant: true}, false},
		{"unknown unit", Row{Name: "Foo-4", Unit: "widgets/op", Delta: 25, Significant: true}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := len(Regressions([]Row{tt.row})) == 1
			if got != tt.wants {
				t.Errorf("Regressions(%+v) flagged=%v, want %v", tt.row, got, tt.wants)
			}
		})
	}
}

func TestSelector(t *testing.T) {
	for _, tt := range []struct {
		name string
		rows []Row
		want string
	}{
		{"top level", []Row{{Name: "Foo-4"}}, "^Benchmark(Foo)$"},
		{"sub-benchmark uses its parent", []Row{{Name: "Foo/Sub-4"}}, "^Benchmark(Foo)$"},
		{"two subs collapse to one parent", []Row{{Name: "Foo/A-4"}, {Name: "Foo/B-4"}}, "^Benchmark(Foo)$"},
		{"sorted and deduplicated", []Row{{Name: "Zed-4"}, {Name: "Abc-4"}, {Name: "Zed-4"}}, "^Benchmark(Abc|Zed)$"},
		{"digits in the name survive", []Row{{Name: "ParseError72-4"}}, "^Benchmark(ParseError72)$"},
		{"same benchmark two units", []Row{{Name: "Foo-4", Unit: "sec/op"}, {Name: "Foo-4", Unit: "B/op"}}, "^Benchmark(Foo)$"},
		{"nothing flagged", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Selector(tt.rows); got != tt.want {
				t.Errorf("Selector() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A name reaching a regex must not be able to change its meaning.
func TestSelectorQuotesMetacharacters(t *testing.T) {
	got := Selector([]Row{{Name: "Foo.Bar+Baz-4"}})
	if strings.Contains(got, "Foo.Bar+Baz") {
		t.Fatalf("metacharacters were not quoted: %s", got)
	}
}

func TestConfirmMatchesNameAndUnit(t *testing.T) {
	flagged := []Key{{Name: "Foo-4", Unit: "sec/op"}}

	sameMetric := []Row{{Name: "Foo-4", Unit: "sec/op", Delta: 25, Significant: true}}
	if got := Confirm(sameMetric, flagged); len(got) != 1 {
		t.Errorf("a reproduced sec/op regression was not confirmed")
	}

	// The flagged timing regression did not reproduce; allocations regressed
	// instead. That is a different result, not a confirmation.
	otherMetric := []Row{
		{Name: "Foo-4", Unit: "sec/op", Delta: 1, Significant: false},
		{Name: "Foo-4", Unit: "B/op", Delta: 25, Significant: true},
	}
	if got := Confirm(otherMetric, flagged); len(got) != 0 {
		t.Errorf("a different metric confirmed the candidate: %+v", got)
	}

	otherBenchmark := []Row{{Name: "Bar-4", Unit: "sec/op", Delta: 25, Significant: true}}
	if got := Confirm(otherBenchmark, flagged); len(got) != 0 {
		t.Errorf("a different benchmark confirmed the candidate: %+v", got)
	}
}

func TestConfirmRequiresReproduction(t *testing.T) {
	flagged := []Key{{Name: "Foo-4", Unit: "sec/op"}}
	notAgain := []Row{{Name: "Foo-4", Unit: "sec/op", Delta: 1.2, Significant: true}}
	if got := Confirm(notAgain, flagged); len(got) != 0 {
		t.Errorf("a 1.2%% second measurement confirmed a 25%% candidate: %+v", got)
	}
}

func TestConfirmMatchesPackage(t *testing.T) {
	flagged := []Key{{Package: "example.com/root", Name: "Foo-4", Unit: "sec/op"}}
	recheck := []Row{
		{Package: "example.com/root", Name: "Foo-4", Unit: "sec/op", Delta: 1, Significant: false},
		{Package: "example.com/root/msdsn", Name: "Foo-4", Unit: "sec/op", Delta: 25, Significant: true},
	}
	if got := Confirm(recheck, flagged); len(got) != 0 {
		t.Errorf("a same-named benchmark in another package confirmed the candidate: %+v", got)
	}
}
