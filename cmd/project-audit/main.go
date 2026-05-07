package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/10morgant/reimagined-enigma/internal/audit"
)

func main() {
	cfg, showHelp, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	if showHelp {
		flag.Usage()
		return
	}

	root, err := filepath.Abs(cfg.RootPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving path: %v\n", err)
		os.Exit(1)
	}
	cfg.RootPath = root

	if stat, err := os.Stat(cfg.RootPath); err != nil || !stat.IsDir() {
		fmt.Fprintf(os.Stderr, "error: path must be an existing directory: %s\n", cfg.RootPath)
		os.Exit(1)
	}

	cutoff, err := audit.ParseAge(cfg.Age, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n  Project Audit")
	fmt.Println("  -----------------------------------------")
	fmt.Printf("  Root path   : %s\n", cfg.RootPath)
	fmt.Printf("  Age cutoff  : %s (before %s)\n", cfg.Age, cutoff.Format("2006-01-02"))
	fmt.Printf("  Filter by   : %s\n", filterLabel(cfg.FilterBy))
	fmt.Printf("  Output file : %s\n", cfg.OutputPath)
	fmt.Printf("  Output type : %s\n", strings.ToUpper(string(cfg.OutputFormat)))
	fmt.Println("  -----------------------------------------")
	fmt.Println()

	progress := &audit.Progress{}
	done := make(chan struct{})
	go progressReporter(progress, done)

	rows, err := audit.Run(context.Background(), cfg, progress)
	close(done)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "audit canceled")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "audit failed: %v\n", err)
		os.Exit(1)
	}

	if len(rows) == 0 {
		fmt.Printf("warning: no files matching the '%s' filter older than '%s' were found.\n", cfg.FilterBy, cfg.Age)
		return
	}

	outputPath, err := audit.WriteOutput(rows, cfg.OutputPath, cfg.OutputFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output to %q: %v\n", cfg.OutputPath, err)
		os.Exit(1)
	}

	fmt.Println("  -----------------------------------------")
	fmt.Println("  Audit complete.")
	fmt.Printf("  Files reported : %d\n", len(rows))
	fmt.Printf("  Output saved   : %s\n", outputPath)
	fmt.Println("  -----------------------------------------")
	fmt.Println()
}

func parseFlags() (audit.Config, bool, error) {
	now := time.Now()
	cfg := audit.Config{
		Age:          "2y",
		FilterBy:     audit.FilterOldest,
		OutputFormat: audit.OutputConfluence,
		OutputPath:   fmt.Sprintf("ProjectAudit_%s.csv", now.Format("20060102_150405")),
		WalkWorkers:  runtime.NumCPU(),
		MetaWorkers:  runtime.NumCPU(),
		Now:          time.Now,
	}

	help := flag.Bool("h", false, "Show help")
	flag.BoolVar(help, "help", false, "Show help")
	flag.StringVar(&cfg.RootPath, "path", "", "Path to the root projects folder (required)")
	flag.StringVar(&cfg.Age, "age", cfg.Age, "Age cutoff (e.g. 2y, 6m, 90d, 3w)")
	filterBy := string(cfg.FilterBy)
	flag.StringVar(&filterBy, "filter-by", filterBy, "Date field to filter on: Created, Modified, Oldest")
	format := string(cfg.OutputFormat)
	flag.StringVar(&format, "output-format", format, "Output format: csv or confluence")
	flag.StringVar(&cfg.OutputPath, "output", cfg.OutputPath, "Output file path")
	flag.IntVar(&cfg.WalkWorkers, "walk-workers", cfg.WalkWorkers, "Number of parallel directory-walk workers")
	flag.IntVar(&cfg.MetaWorkers, "meta-workers", cfg.MetaWorkers, "Number of parallel metadata workers")
	flag.Parse()

	if *help {
		return cfg, true, nil
	}
	if strings.TrimSpace(cfg.RootPath) == "" {
		return cfg, false, errors.New("-path is required")
	}
	cfg.FilterBy = normalizeFilter(filterBy)
	if err := audit.ValidateFilter(cfg.FilterBy); err != nil {
		return cfg, false, err
	}
	cfg.OutputFormat = audit.OutputFormat(strings.ToLower(strings.TrimSpace(format)))
	if cfg.OutputFormat != audit.OutputCSV && cfg.OutputFormat != audit.OutputConfluence {
		return cfg, false, fmt.Errorf("unsupported output format %q", format)
	}
	return cfg, false, nil
}

func normalizeFilter(v string) audit.FilterMode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "created":
		return audit.FilterCreated
	case "modified":
		return audit.FilterModified
	default:
		return audit.FilterOldest
	}
}

func filterLabel(mode audit.FilterMode) string {
	switch mode {
	case audit.FilterCreated:
		return "Created date"
	case audit.FilterModified:
		return "Last modified date"
	default:
		return "Oldest of created / last modified"
	}
}

func progressReporter(progress *audit.Progress, done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	print := func(final bool) {
		projectsDone, projectsTotal, filesFound, filesProcessed, filesMatched := progress.Snapshot()
		msg := fmt.Sprintf("\rProgress: projects %d/%d | files discovered %d | files processed %d | matches %d", projectsDone, projectsTotal, filesFound, filesProcessed, filesMatched)
		if final {
			fmt.Fprintln(os.Stderr, msg)
			return
		}
		fmt.Fprint(os.Stderr, msg)
	}

	for {
		select {
		case <-done:
			print(true)
			return
		case <-ticker.C:
			print(false)
		}
	}
}
