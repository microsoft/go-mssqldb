// Command benchgate decides whether a benchstat comparison contains a real
// performance regression. It reads benchstat's CSV output rather than its table
// so that units and row names are fields instead of layout.
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RegressionThreshold is the percentage change treated as a regression, on top
// of benchstat's own significance test.
const RegressionThreshold = 15.0

// Row is one benchmark measurement in one unit.
type Row struct {
	Name        string
	Unit        string
	Delta       float64 // percent, as benchstat signs it
	Significant bool
}

// Key identifies a measurement. A benchmark appears once per unit, and those
// are separate results: sec/op regressing is not B/op regressing.
type Key struct {
	Name string
	Unit string
}

func (r Row) Key() Key { return Key{r.Name, r.Unit} }

// unitDirection maps a benchstat unit to whether smaller is better. Units are
// listed explicitly because guessing a direction is worse than not knowing one:
// an unlisted lower-is-better metric such as errors/op would have a rise
// reported as an improvement and a fall fail the build.
var unitDirection = map[string]bool{
	"sec/op":    true,
	"ns/op":     true,
	"B/op":      true,
	"allocs/op": true,
	"B/s":       false,
	"MB/s":      false,
}

func lowerIsBetter(unit string) bool { return unitDirection[unit] }

var deltaPattern = regexp.MustCompile(`^[+-][0-9]+(\.[0-9]+)?%$`)

// Parse reads benchstat -format=csv output. Rows outside a recognised table are
// ignored, and rows whose delta it cannot read are an error rather than a silent
// drop, so format drift cannot hide a regression behind the rows that did parse.
// Benchmarks present on only one side carry no delta and are skipped.
func Parse(r io.Reader) ([]Row, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	var rows []Row
	unit := ""
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv: %w", err)
		}
		switch {
		case len(rec) >= 3 && rec[0] == "" && rec[2] == "CI":
			unit = rec[1]
			continue
		case len(rec) > 0 && rec[0] == "":
			// File-list line between tables; the next header sets the unit again.
			unit = ""
			continue
		}
		if unit == "" || len(rec) < 6 || rec[0] == "geomean" {
			continue
		}
		row := Row{Name: rec[0], Unit: unit}
		switch d := strings.TrimSpace(rec[5]); {
		case d == "":
			// Benchmark present on only one side, so there is nothing to compare.
			continue
		case d == "~":
			row.Significant = false
		case deltaPattern.MatchString(d):
			v, err := strconv.ParseFloat(strings.TrimSuffix(d, "%"), 64)
			if err != nil {
				// The delta has the expected shape but will not convert, so our
				// own assumption is broken. Fail closed rather than drop the row.
				return nil, fmt.Errorf("delta %q for %s: %w", d, rec[0], err)
			}
			row.Delta, row.Significant = v, true
		default:
			// Dropping this row would hide a regression behind the rows that did
			// parse, so treat unreadable output as format drift and fail closed.
			return nil, fmt.Errorf("unreadable delta %q for %s (%s)", d, rec[0], unit)
		}
		// A significant change in a unit with no known direction cannot be
		// classified, and guessing would invert it. An insignificant row is
		// harmless whatever the unit, so it does not need a direction.
		if _, known := unitDirection[unit]; !known && row.Significant {
			return nil, fmt.Errorf("unit %q for %s has no known direction; add it to unitDirection in internal/benchgate", unit, rec[0])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Regressions returns significant changes at least RegressionThreshold worse.
// Worse means larger for units like sec/op and smaller for throughput.
func Regressions(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if !r.Significant {
			continue
		}
		if lowerIsBetter(r.Unit) && r.Delta >= RegressionThreshold ||
			!lowerIsBetter(r.Unit) && r.Delta <= -RegressionThreshold {
			out = append(out, r)
		}
	}
	return out
}

// Improvements is the mirror of Regressions, and knows a rising B/s is good.
func Improvements(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if !r.Significant {
			continue
		}
		if lowerIsBetter(r.Unit) && r.Delta < 0 || !lowerIsBetter(r.Unit) && r.Delta > 0 {
			out = append(out, r)
		}
	}
	return out
}

var procSuffix = regexp.MustCompile(`-[0-9]+$`)

// Selector builds a -bench expression for the benchmarks owning these rows.
// go test splits -bench at '/' and matches each element separately, so a
// sub-benchmark is addressed through its parent.
func Selector(rows []Row) string {
	seen := map[string]bool{}
	var names []string
	for _, r := range rows {
		n := procSuffix.ReplaceAllString(r.Name, "")
		if i := strings.Index(n, "/"); i >= 0 {
			n = n[:i]
		}
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, regexp.QuoteMeta(n))
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "^Benchmark(" + strings.Join(names, "|") + ")$"
}

// Confirm returns the flagged measurements that regressed again. Matching is by
// name and unit: the same benchmark regressing in a different metric is not the
// candidate reproducing.
func Confirm(recheck []Row, flagged []Key) []Row {
	want := make(map[Key]bool, len(flagged))
	for _, k := range flagged {
		want[k] = true
	}
	var out []Row
	for _, r := range Regressions(recheck) {
		if want[r.Key()] {
			out = append(out, r)
		}
	}
	return out
}

// Unmeasured returns flagged keys the recheck produced no comparable row for.
// Siblings of a flagged sub-benchmark keep the recheck non-empty, so without
// this a candidate that never ran again looks the same as one that came back
// clean, and would be dismissed as runner noise.
func Unmeasured(recheck []Row, flagged []Key) []Key {
	seen := make(map[Key]bool, len(recheck))
	for _, r := range recheck {
		seen[r.Key()] = true
	}
	var out []Key
	for _, k := range flagged {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}
