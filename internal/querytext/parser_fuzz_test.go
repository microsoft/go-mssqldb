package querytext

import "testing"

// FuzzParseParams exercises the legacy query parameter rewriter with
// arbitrary query text. ParseParams must never panic.
func FuzzParseParams(f *testing.F) {
	seeds := []string{
		"select * from t where id = ?",
		"select ? , ? , ?",
		"select '?' as literal",
		"select /* ? */ 1",
		"select -- ?\n 1",
		"select [?] from t",
		"select \"?\" from t",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(_ *testing.T, query string) {
		_, _ = ParseParams(query)
	})
}
