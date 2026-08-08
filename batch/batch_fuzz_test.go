// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package batch

import "testing"

// FuzzSplit exercises the batch splitter with arbitrary SQL text and
// separators. The splitter must never panic regardless of input.
func FuzzSplit(f *testing.F) {
	seeds := []struct {
		sql string
		sep string
	}{
		{"select 1\nGO\nselect 2", "GO"},
		{"select 1 -- comment\nGO 3\nselect 2", "GO"},
		{"select '\\\nvalue'\nGO", "GO"},
		{"/* multi\nline */ GO", "GO"},
		{"goto next", "GO"},
		{"", "GO"},
		{"GO", ""},
	}
	for _, s := range seeds {
		f.Add(s.sql, s.sep)
	}

	f.Fuzz(func(t *testing.T, sql, sep string) {
		_ = Split(sql, sep)
	})
}
