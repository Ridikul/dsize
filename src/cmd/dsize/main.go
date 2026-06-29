// dsize — disk usage scanner with optional web UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"dsize/internal/assets"
	"dsize/internal/history"
	"dsize/internal/scan"
	"dsize/internal/server"
)

func main() {
	os.Exit(run())
}

func run() int {
	// --- Flag definitions ---
	uiFlag := flag.Bool("ui", false, "Start local HTTP UI server and open browser")
	uiPort := flag.Int("ui-port", 8420, "Port for the UI server (1-65535)")
	maxDepth := flag.Int("max-depth", 0, "Maximum directory depth (0=unlimited)")
	topN := flag.Int("top", 0, "Show only the top N entries (0=all)")
	useBytes := flag.Bool("bytes", false, "Print raw byte counts instead of IEC units")
	noColor := flag.Bool("no-color", false, "Disable ANSI colour in CLI output")
	jsonOut := flag.Bool("json", false, "Output results as JSON to stdout")
	crossMounts := flag.Bool("cross-mounts", false, "Descend into other mounted filesystems (default: stay on the root's filesystem, like du -x)")
	apparentSize := flag.Bool("apparent-size", false, "Sum logical file sizes instead of on-disk allocated size (default: real disk usage, like du)")

	flag.Parse()

	// --- Determine scan target ---
	// A positional arg is the directory to scan. Without one the CLI defaults
	// to ".", but the UI starts with no folder — its welcome screen lets the
	// user pick one (an empty target leaves the scan root unset).
	var absTarget string
	if flag.NArg() > 0 {
		var err error
		absTarget, err = resolveAbsPath(flag.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	} else if !*uiFlag {
		absTarget, _ = resolveAbsPath(".")
	}

	// --- Mutual-exclusion check (FR-1.6) ---
	if *uiFlag {
		if *jsonOut {
			fmt.Fprintln(os.Stderr, "error: --ui and --json are mutually exclusive")
			return 1
		}
		if *noColor {
			fmt.Fprintln(os.Stderr, "error: --ui and --no-color are mutually exclusive")
			return 1
		}
	}

	// --- Port validation (FR-1.3) ---
	if *uiFlag && (*uiPort < 1 || *uiPort > 65535) {
		fmt.Fprintln(os.Stderr, "error: invalid port (must be 1-65535)")
		return 1
	}

	opts := scan.Options{
		MaxDepth:     *maxDepth,
		TopN:         *topN,
		Bytes:        *useBytes,
		CrossMounts:  *crossMounts,
		ApparentSize: *apparentSize,
	}

	// --- UI mode ---
	if *uiFlag {
		return runUI(absTarget, *uiPort, opts)
	}

	// --- CLI mode (US-8): unchanged behaviour ---
	return runCLI(absTarget, opts, *jsonOut, *noColor, *useBytes)
}

// runUI starts the HTTP server, opens the browser, and blocks until interrupted.
func runUI(target string, port int, opts scan.Options) int {
	assetFS := assets.FS()

	scanFunc := func(ctx context.Context, root string, onProgress func(scan.Progress)) (scan.Result, error) {
		return scan.ScanStream(ctx, root, opts, onProgress)
	}

	srv := server.New(assetFS, target, scanFunc)
	url, err := srv.Start(port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not start server: %v\n", err)
		return 1
	}

	// Open browser (FR-1.4); failure is non-fatal.
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "info: could not open browser (%v); visit %s\n", err, url)
	}

	// Block until SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return 0
}

// runCLI performs the scan and prints results to stdout (no UI).
func runCLI(target string, opts scan.Options, jsonOut, noColor, useBytes bool) int {
	result, err := scan.ScanStream(context.Background(), target, opts, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Write history snapshot (non-fatal on failure, FR-7.6).
	saveSnapshot(result, target)

	if jsonOut {
		printJSON(result, target)
		return 0
	}
	printText(result, useBytes, noColor)
	return 0
}

// saveSnapshot persists a history snapshot, logging any error to stderr.
func saveSnapshot(result scan.Result, target string) {
	entries := make([]history.Entry, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = history.Entry{Path: e.Path, Size: e.Size, IsDir: e.IsDir}
	}
	snap := history.Snapshot{
		Version:    1,
		ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		Target:     target,
		TotalSize:  result.TotalSize,
		FileCount:  result.FileCount,
		DirCount:   result.DirCount,
		DurationMs: result.DurationMs,
		Entries:    entries,
	}
	if err := history.Write(snap); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write history snapshot: %v\n", err)
	}
}

// printText outputs the scan results as coloured text to stdout.
func printText(result scan.Result, useBytes, noColor bool) {
	maxSize := int64(0)
	for _, e := range result.Entries {
		if e.Size > maxSize {
			maxSize = e.Size
		}
	}
	const barWidth = 40
	for _, e := range result.Entries {
		label := e.Path
		sizeStr := scan.FormatSize(e.Size, useBytes)
		width := 1
		if maxSize > 0 {
			width = int(float64(e.Size) / float64(maxSize) * barWidth)
			if width < 1 {
				width = 1
			}
		}
		bar := repeatChar('#', width)

		if !noColor && e.IsDir {
			// Blue for dirs.
			fmt.Printf("\033[34m%-50s\033[0m %10s [%s]\n", label, sizeStr, bar)
		} else {
			fmt.Printf("%-50s %10s [%s]\n", label, sizeStr, bar)
		}
	}
	fmt.Printf("\nTotal: %s in %d files, %d dirs\n",
		scan.FormatSize(result.TotalSize, useBytes), result.FileCount, result.DirCount)
}

// printJSON outputs the scan result as JSON to stdout.
func printJSON(result scan.Result, target string) {
	type jsonEntry struct {
		Path  string `json:"path"`
		Size  int64  `json:"size"`
		IsDir bool   `json:"isDir"`
	}
	type jsonOutput struct {
		Target     string      `json:"target"`
		TotalSize  int64       `json:"totalSize"`
		FileCount  int64       `json:"fileCount"`
		DirCount   int64       `json:"dirCount"`
		DurationMs int64       `json:"durationMs"`
		Entries    []jsonEntry `json:"entries"`
	}

	entries := make([]jsonEntry, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = jsonEntry{e.Path, e.Size, e.IsDir}
	}
	out := jsonOutput{
		Target:     target,
		TotalSize:  result.TotalSize,
		FileCount:  result.FileCount,
		DirCount:   result.DirCount,
		DurationMs: result.DurationMs,
		Entries:    entries,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// openBrowser launches the default system browser (FR-1.4).
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default: // Linux/BSD
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}

func resolveAbsPath(p string) (string, error) {
	if p == "" || p == "." {
		return os.Getwd()
	}
	return p, nil
}

func repeatChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
