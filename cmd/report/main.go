package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/logutil"
)

func main() {
	days := flag.Int("days", 30, "number of days to look back in the report")
	flag.Parse()

	log := logutil.New(&slog.HandlerOptions{Level: slog.LevelInfo})

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	bc := broker.New(cfg.Alpaca, log)

	log.Info("building report", "days", *days)
	data, err := buildReport(bc, cfg, *days)
	if err != nil {
		log.Error("failed to build report", "error", err)
		os.Exit(1)
	}

	// Output to reports/YYYY-MM-DD/report.html
	dateDir := time.Now().Format("2006-01-02")
	outDir := filepath.Join("reports", dateDir)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Error("failed to create report directory", "error", err)
		os.Exit(1)
	}

	data.OtherReports = scanReports("reports", dateDir)

	outPath := filepath.Join(outDir, "report.html")
	if err := writeReport(data, outPath); err != nil {
		log.Error("failed to write report", "error", err)
		os.Exit(1)
	}

	absPath, _ := filepath.Abs(outPath)
	fmt.Printf("✓  Report saved: %s\n", absPath)
	openBrowser(absPath)
}

// scanReports returns links to every dated report folder except the current one.
func scanReports(reportsDir, currentDir string) []ReportLink {
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		return nil
	}
	var links []ReportLink
	for _, e := range entries {
		if !e.IsDir() || e.Name() == currentDir {
			continue
		}
		if _, err := os.Stat(filepath.Join(reportsDir, e.Name(), "report.html")); err != nil {
			continue
		}
		links = append(links, ReportLink{
			Date: e.Name(),
			Path: fmt.Sprintf("../%s/report.html", e.Name()),
		})
	}
	// Newest first
	sort.Slice(links, func(i, j int) bool { return links[i].Date > links[j].Date })
	return links
}

func openBrowser(absPath string) {
	url := "file://" + absPath
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("  (could not open browser automatically: %v)\n", err)
	}
}
