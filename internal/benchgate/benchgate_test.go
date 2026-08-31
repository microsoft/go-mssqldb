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
	if rows[0] != (Row{Name: "Foo-4", Unit: "sec/op", Delta: 25, Significant: true}) {
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
// Returning none makes the caller fail closed instead of reporting "clean".
func TestParseFailsClosedOnHeaderDrift(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,Confidence,sec/op,Confidence,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,+22.00%,p=0.000 n=10
`
	if rows := parseString(t, in); len(rows) != 0 {
		t.Fatalf("got %d rows, want 0: %+v", len(rows), rows)
	}
}

func TestParseFailsClosedOnUnreadableDelta(t *testing.T) {
	const in = `,old,,new,,,
,sec/op,CI,sec/op,CI,vs base,P
Foo-4,1e-07,0%,1.25e-07,0%,20 percent worse,p=0.000 n=10
`
	if rows := parseString(t, in); len(rows) != 0 {
		t.Fatalf("got %d rows, want 0: %+v", len(rows), rows)
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
		{"above threshold", Row{"Foo-4", "sec/op", 25, true}, true},
		{"exactly at threshold", Row{"Foo-4", "sec/op", 15, true}, true},
		{"below threshold", Row{"Foo-4", "sec/op", 14.99, true}, false},
		{"not significant", Row{"Foo-4", "sec/op", 25, false}, false},
		{"allocs count", Row{"Foo-4", "allocs/op", 25, true}, true},
		{"bytes per op", Row{"Foo-4", "B/op", 25, true}, true},
		{"throughput", Row{"Foo-4", "B/s", 25, true}, false},
		{"unknown unit", Row{"Foo-4", "widgets/op", 25, true}, false},
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
