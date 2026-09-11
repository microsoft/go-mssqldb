package mssql

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
)

// Exercise the assignments in both SQL-container jobs without starting a container.
func TestCISQLPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SQL-container CI jobs run on Unix")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("CI password generation requires bash")
	}
	workflow, err := os.ReadFile(".github/workflows/pr-validation.yml")
	if err != nil {
		t.Fatal(err)
	}
	assignments := regexp.MustCompile(`(?m)^\s*((?:export )?SQLCMDPASSWORD=.+)$`).FindAllStringSubmatch(string(workflow), -1)
	if len(assignments) != 2 {
		t.Fatal("expected password assignments for the build and benchmarks jobs")
	}

	for i, assignment := range assignments {
		for _, tc := range []struct {
			name   string
			output string
			fail   bool
		}{
			{name: "letters", output: strings.Repeat("a", 28)},
			{name: "digits", output: strings.Repeat("0", 28)},
			{name: "generator failure", fail: true},
		} {
			t.Run(strconv.Itoa(i)+"/"+tc.name, func(t *testing.T) {
				bin := t.TempDir()
				body := "printf '%s\n' '" + tc.output + "'\n"
				if tc.fail {
					body = "exit 23\n"
				}
				if err := os.WriteFile(filepath.Join(bin, "openssl"), []byte("#!/bin/sh\n[ \"$*\" = 'rand -hex 14' ] || exit 2\n"+body), 0700); err != nil {
					t.Fatal(err)
				}
				// A deterministic hex digest whose base64 representation has no digits.
				// This makes the previous date/hash/base64 generator fail reliably.
				legacyHash := "#!/bin/sh\ncat >/dev/null\nprintf '%s\n' '" + strings.Repeat("a", 64) + "  -'\n"
				if err := os.WriteFile(filepath.Join(bin, "sha256sum"), []byte(legacyHash), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, bash, "-euo", "pipefail", "-c", strings.TrimSpace(assignment[1])+"\nprintf '%s' \"$SQLCMDPASSWORD\"")
				output, err := cmd.Output()
				if tc.fail {
					exitError, ok := err.(*exec.ExitError)
					if !ok || exitError.ExitCode() != 23 || ctx.Err() != nil || len(output) != 0 {
						t.Fatal("password generation must preserve the random source failure without output")
					}
					return
				}
				if err != nil {
					t.Fatal("password generation failed:", err)
				}
				password := string(output)
				if len(password) != 32 {
					t.Fatal("password must remain 32 characters long")
				}
				for _, pattern := range []string{`[A-Z]`, `[a-z]`, `[0-9]`, `[^a-zA-Z0-9]`} {
					if !regexp.MustCompile(pattern).MatchString(password) {
						t.Fatalf("password lacks character class %s", pattern)
					}
				}
				config, err := msdsn.Parse("sqlserver://sa:" + password + "@localhost:1433?database=master")
				if err != nil || config.Password != password {
					t.Fatal("CI password must round-trip through the driver's DSN parser")
				}
			})
		}
	}
}
