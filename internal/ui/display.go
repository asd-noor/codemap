// Package ui provides terminal UI components for CodeMap.
package ui

import (
	"fmt"
)

// Colors for terminal output (ANSI codes)
const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
	ColorGray   = "\033[37m"
)

// spinnerFrames represents the animation frames for the spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Spinner struct {
	frames []string
	index  int
}

func NewSpinner() *Spinner {
	return &Spinner{
		frames: spinnerFrames,
		index:  0,
	}
}

// Frame returns the next spinner frame.
func (s *Spinner) Frame() string {
	frame := s.frames[s.index%len(s.frames)]
	s.index++
	return frame
}

// Banner prints the CodeMap banner.
func Banner() {
	fmt.Printf("\n")
	fmt.Printf("%s%s ▂▃▅▆▇█▓░ CodeMap ░▓█▇▆▅▃▂%s\n", ColorBold, ColorCyan, ColorReset)
	fmt.Printf("%s  Code Exploration Engine — MCP Server%s\n", ColorDim, ColorReset)
	fmt.Printf("\n")
}

// StatusStarting prints initial startup message.
func StatusStarting(projectDir, dbPath string) {
	fmt.Printf("%s➤%s Starting CodeMap\n", ColorBlue, ColorReset)
	fmt.Printf("  %sProject:%s %s\n", ColorDim, ColorReset, projectDir)
	fmt.Printf("  %sDatabase:%s %s\n", ColorDim, ColorReset, dbPath)
}

// StatusIndexing prints indexing in-progress message with spinner.
func StatusIndexing(frame string) {
	fmt.Printf("\r%s%s%s Indexing codebase...%s", ColorYellow, frame, ColorReset, ColorReset)
}

// StatusIndexed prints completion message with statistics.
func StatusIndexed(nodes, edges int) {
	fmt.Printf("\r%s✓%s Indexing complete%s\n", ColorGreen, ColorReset, ColorReset)
	fmt.Printf("  %sNodes:%s %d  %sEdges:%s %d\n",
		ColorDim, ColorReset, nodes,
		ColorDim, ColorReset, edges)
}

// StatusEnriching prints LSP enrichment message.
func StatusEnriching(frame string) {
	fmt.Printf("\r%s%s%s Enriching with LSP cross-references...%s", ColorYellow, frame, ColorReset, ColorReset)
}

// StatusEnriched prints LSP enrichment completion.
func StatusEnriched(edges int) {
	fmt.Printf("\r%s✓%s LSP enrichment complete%s\n", ColorGreen, ColorReset, ColorReset)
	fmt.Printf("  %sEdges added:%s %d\n",
		ColorDim, ColorReset, edges)
}

// StatusReady prints server ready message.
func StatusReady() {
	fmt.Printf("\n%s✓%s Server ready%s — MCP tools registered\n\n", ColorGreen, ColorReset, ColorReset)
}

// StatusFailed prints error message.
func StatusFailed(err error) {
	fmt.Printf("\r%s✗%s Indexing failed%s\n", ColorReset, ColorReset, ColorReset)
	fmt.Printf("  %s%v%s\n", ColorReset, err, ColorReset)
}

// StatusWatcherStarted prints watcher startup message.
func StatusWatcherStarted(pidPath string) {
	fmt.Printf("%s✓%s Watcher started%s\n", ColorGreen, ColorReset, ColorReset)
	fmt.Printf("  %sPID file:%s %s\n", ColorDim, ColorReset, pidPath)
}

// StatusDaemonAlreadyRunning prints message when daemon is already running.
func StatusDaemonAlreadyRunning(pidPath string) {
	fmt.Printf("%s⚠%s  Daemon already running%s\n", ColorYellow, ColorReset, ColorReset)
	fmt.Printf("  %sPID file:%s %s\n", ColorDim, ColorReset, pidPath)
}
