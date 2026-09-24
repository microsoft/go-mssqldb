package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Exit codes the workflow branches on.
const (
	exitClean   = 0
	exitFailure = 1 // could not evaluate; fail closed
	exitFound   = 2 // regressions detected or reproduced
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run holds everything main does apart from exiting, so it can be tested.
func run(args []string, out io.Writer) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(out, "usage: benchgate detect|confirm [flags]")
		return exitFailure
	}
	var err error
	var code int
	switch args[0] {
	case "detect":
		code, err = detect(args[1:])
	case "confirm":
		code, err = confirm(args[1:])
	default:
		err = fmt.Errorf("unknown subcommand %q", args[0])
		code = exitFailure
	}
	if err != nil {
		_, _ = fmt.Fprintf(out, "::error::%v\n", err)
		return exitFailure
	}
	return code
}

func parseFile(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Parse(f)
}

func detect(args []string) (int, error) {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	csvPath := fs.String("csv", "", "benchstat -format=csv output")
	flaggedPath := fs.String("flagged", "", "file to write flagged package/name/unit keys to")
	selectorPath := fs.String("selector", "", "file to write the -bench selector to")
	if err := fs.Parse(args); err != nil {
		return exitFailure, err
	}
	rows, err := parseFile(*csvPath)
	if err != nil {
		return exitFailure, err
	}
	if len(rows) == 0 {
		return exitFailure, fmt.Errorf("no comparable rows in %s; benchstat output not recognised", *csvPath)
	}
	fmt.Printf("Parsed %d benchmark rows.\n", len(rows))

	if imp := Improvements(rows); len(imp) > 0 {
		for _, r := range imp {
			fmt.Printf("  improved: %s %+.2f%% %s\n", r.Name, r.Delta, r.Unit)
		}
		fmt.Println("::notice::Performance improvements detected (see above)")
	}

	reg := Regressions(rows)
	if len(reg) == 0 {
		fmt.Println("No significant regressions detected.")
		return exitClean, nil
	}
	for _, r := range reg {
		fmt.Printf("  candidate: %s %+.2f%% %s\n", r.Name, r.Delta, r.Unit)
	}
	if *flaggedPath != "" {
		if err := writeFlagged(*flaggedPath, reg); err != nil {
			return exitFailure, err
		}
	}
	if *selectorPath != "" {
		if err := os.WriteFile(*selectorPath, []byte(Selector(reg)+"\n"), 0o644); err != nil {
			return exitFailure, err
		}
	}
	return exitFound, nil
}

func confirm(args []string) (int, error) {
	fs := flag.NewFlagSet("confirm", flag.ContinueOnError)
	csvPath := fs.String("csv", "", "benchstat -format=csv output for the confirmation run")
	flaggedPath := fs.String("flagged", "", "package/name/unit keys written by detect")
	if err := fs.Parse(args); err != nil {
		return exitFailure, err
	}
	rows, err := parseFile(*csvPath)
	if err != nil {
		return exitFailure, err
	}
	if len(rows) == 0 {
		return exitFailure, fmt.Errorf("no comparable rows in %s; confirmation run produced nothing to evaluate", *csvPath)
	}
	flagged, err := readFlagged(*flaggedPath)
	if err != nil {
		return exitFailure, err
	}
	if len(flagged) == 0 {
		return exitFailure, fmt.Errorf("no flagged rows in %s", *flaggedPath)
	}
	got := Confirm(rows, flagged)
	if len(got) > 0 {
		for _, r := range got {
			fmt.Printf("  reproduced: %s %+.2f%% %s\n", r.Name, r.Delta, r.Unit)
		}
		return exitFound, nil
	}
	if un := Unmeasured(rows, flagged); len(un) > 0 {
		names := make([]string, len(un))
		for i, k := range un {
			names[i] = fmt.Sprintf("%s (%s)", k.Name, k.Unit)
		}
		return exitFailure, fmt.Errorf("confirmation run did not remeasure %s; the candidate is unevaluated, not noise", strings.Join(names, ", "))
	}
	fmt.Println("::warning::Flagged regression did not reproduce; treating the first measurement as runner noise.")
	return exitClean, nil
}

func writeFlagged(path string, rows []Row) error {
	var b strings.Builder
	for _, r := range rows {
		_, _ = fmt.Fprintf(&b, "%s\t%s\t%s\n", r.Package, r.Name, r.Unit)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func readFlagged(path string) ([]Key, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var keys []Key
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		keys = append(keys, Key{parts[0], parts[1], parts[2]})
	}
	return keys, sc.Err()
}
