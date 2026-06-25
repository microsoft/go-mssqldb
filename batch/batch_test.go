// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package batch

import (
	"fmt"
	"testing"
)

func TestBatchSplit(t *testing.T) {
	type testItem struct {
		Sql    string
		Expect []string
	}

	list := []testItem{
		testItem{
			Sql: `use DB
go
select 1
go
select 2
`,
			Expect: []string{`use DB
`, `
select 1
`, `
select 2
`,
			},
		},
		testItem{
			Sql: `go
use DB go
`,
			Expect: []string{`
use DB go
`,
			},
		},
		testItem{
			Sql: `select 'It''s go time'
go
select top 1 1`,
			Expect: []string{`select 'It''s go time'
`, `
select top 1 1`,
			},
		},
		testItem{
			Sql: `select 1 /* go */
go
select top 1 1`,
			Expect: []string{`select 1 /* go */
`, `
select top 1 1`,
			},
		},
		testItem{
			Sql: `select 1 -- go
go
select top 1 1`,
			Expect: []string{`select 1 -- go
`, `
select top 1 1`,
			},
		},
		testItem{
			Sql: `PRINT 1
GOTO Bookmark
GO
PRINT 2
Bookmark:
GO`,
			Expect: []string{"PRINT 1\nGOTO Bookmark\n", "\nPRINT 2\nBookmark:\n"},
		},
		testItem{
			Sql: `
create table t (
    id      int,
    gone_ts datetime
)
go
select
    gone_ts
from test_table
go`,
			Expect: []string{`
create table t (
    id      int,
    gone_ts datetime
)
`, `
select
    gone_ts
from test_table
`,
			},
		},
		testItem{Sql: `"0'"`, Expect: []string{`"0'"`}},
		testItem{Sql: "0'", Expect: []string{"0'"}},
		testItem{Sql: "--", Expect: []string{"--"}},
		testItem{Sql: "GO", Expect: nil},
		testItem{Sql: "/*", Expect: []string{"/*"}},
		testItem{Sql: "gO\x01\x00O550655490663051008\n", Expect: []string{"\n"}},
		testItem{Sql: "select 1;\nGO  2\nselect 2;", Expect: []string{"select 1;\n", "select 1;\n", "\nselect 2;"}},
		testItem{Sql: "select 'hi\\\n-hello';", Expect: []string{"select 'hi-hello';"}},
		testItem{Sql: "select 'hi\\\r\n-hello';", Expect: []string{"select 'hi-hello';"}},
		testItem{Sql: "select 'hi\\\r-hello';", Expect: []string{"select 'hi-hello';"}},
		testItem{Sql: "select 'hi\\\n\nhello';", Expect: []string{"select 'hi\nhello';"}},
		testItem{Sql: "select\ngone_ts\nfrom t;", Expect: []string{"select\ngone_ts\nfrom t;"}},
	}

	index := -1

	for i := range list {
		if index >= 0 && index != i {
			continue
		}
		sqltext := list[i].Sql
		t.Run(fmt.Sprintf("index-%d", i), func(t *testing.T) {
			ss := Split(sqltext, "go")
			if len(ss) != len(list[i].Expect) {
				t.Errorf("Test Item index %d; expect %d items, got %d %q", i, len(list[i].Expect), len(ss), ss)
				return
			}
			for j := 0; j < len(ss); j++ {
				if ss[j] != list[i].Expect[j] {
					t.Errorf("Test Item index %d, batch index %d; expect <%s>, got <%s>", i, j, list[i].Expect[j], ss[j])
				}
			}
		})
	}
}

func TestHasPrefixFold(t *testing.T) {
	list := []struct {
		s, pre string
		is     bool
	}{
		{"h", "H", true},
		{"h", "K", false},
		{"go 5\n", "go", true},
		// Word-boundary checks: separator must not be followed by another letter.
		{"GOTO foo", "GO", false},
		{"gotoflag", "go", false},
		{"GO1\n", "GO", true},
		{"GO_FOO\n", "GO", true},
		// Multi-byte UTF-8 follower. Hebrew aleph (U+05D0) is encoded as
		// 0xD7 0x90; a bare rune(s[i]) cast would see 0xD7 (× MULTIPLICATION
		// SIGN, not a letter) and incorrectly allow the match. Decoding the
		// rune correctly sees U+05D0 (a letter) and rejects.
		{"GO\u05D0test", "GO", false},
		// Latin-1 letter follower (single-byte path).
		{"GOé", "GO", false},
	}
	for _, item := range list {
		is := hasPrefixFold(item.s, item.pre)
		if is != item.is {
			t.Errorf("want (%q, %q)=%t got %t", item.s, item.pre, item.is, is)
		}
	}
}
