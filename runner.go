package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const (
	StatusRunning = "RUNNING"
	StatusDone    = "✅️"
	StatusError   = "❌"
	StatusKilled  = "☠️"
)

const TitleSeparator = "::"
const LabelWidth = 20

var SpinnerFrames = []string{"⠋ ", "⠙ ", "⠹ ", "⠸ ", "⠼ ", "⠴ ", "⠦ ", "⠧ ", "⠇ ", "⠏ "}

const (
	StatusCol   = 50
	MaxCmdWidth = 45
)

// Entry represents a command with an optional custom title.
type Entry struct {
	Title   string
	Command string
}

// commandPlaceholderRe matches a word immediately preceding %% (e.g. basename%%)
var commandPlaceholderRe = regexp.MustCompile(`(\w+)%%`)

// expandCommandPlaceholders replaces word%% patterns by running the word as a
// command with the value as an argument, and replaces plain %% with the value.
func expandCommandPlaceholders(template string, value string) string {
	// First, replace command%% patterns
	result := commandPlaceholderRe.ReplaceAllStringFunc(template, func(match string) string {
		cmd := match[:len(match)-2] // strip trailing %%
		out, err := exec.Command(cmd, value).Output()
		if err != nil {
			return value // fallback to raw value on error
		}
		return strings.TrimRight(string(out), "\n")
	})
	// Then, replace any remaining plain %%
	result = strings.ReplaceAll(result, "%%", value)
	return result
}

// ExpandTemplate generates command args from a template and input lines.
func ExpandTemplate(template string, lines []string) []string {
	var result []string
	for _, line := range lines {
		if idx := strings.Index(template, TitleSeparator); idx >= 0 {
			// Template has an explicit title part
			titleTemplate := template[:idx]
			cmdTemplate := template[idx+len(TitleSeparator):]
			title := expandCommandPlaceholders(titleTemplate, line)
			cmd := expandCommandPlaceholders(cmdTemplate, line)
			result = append(result, title+TitleSeparator+cmd)
		} else {
			cmd := expandCommandPlaceholders(template, line)
			result = append(result, line+TitleSeparator+cmd)
		}
	}
	return result
}

// ParseEntry parses a command argument, extracting an optional title.
func ParseEntry(arg string) Entry {
	if idx := strings.Index(arg, TitleSeparator); idx >= 0 {
		return Entry{
			Title:   strings.TrimSpace(arg[:idx]),
			Command: strings.TrimSpace(arg[idx+len(TitleSeparator):]),
		}
	}
	return Entry{Title: arg, Command: arg}
}

// ParseEntries parses all arguments and validates title uniqueness.
func ParseEntries(args []string) ([]Entry, error) {
	entries := make([]Entry, len(args))
	seen := make(map[string]bool)
	for i, arg := range args {
		entries[i] = ParseEntry(arg)
		if seen[entries[i].Title] {
			return nil, fmt.Errorf("duplicate title: %q", entries[i].Title)
		}
		seen[entries[i].Title] = true
	}
	return entries, nil
}

// WrapText wraps text to the given width without splitting words.
func WrapText(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	var lines []string
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
		} else if len(current)+1+len(word) <= width {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// FormatLabel pads/truncates a title to LabelWidth.
func FormatLabel(title string) string {
	if len(title) >= LabelWidth {
		return title[:LabelWidth]
	}
	return title + strings.Repeat(" ", LabelWidth-len(title))
}

// GetTerminalWidth returns the terminal width or a default.
func GetTerminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// OutputWidth returns usable width (terminal width minus 2 for margins).
func OutputWidth() int {
	w := GetTerminalWidth()
	return w - 2
}

// StatusColumns returns how many columns fit in the terminal width.
func StatusColumns(termWidth int) int {
	colWidth := StatusCol + 4 // title + padding + status icon
	// Account for dividers: cols-1 dividers of 3 chars each (" │ ")
	// cols * colWidth + (cols-1)*3 <= termWidth
	// cols * (colWidth + 3) <= termWidth + 3
	cols := (termWidth + 3) / (colWidth + 3)
	if cols < 1 {
		cols = 1
	}
	return cols
}

// StatusLines returns the number of terminal lines needed for the status list.
func StatusLines(entries []Entry) int {
	cols := StatusColumns(GetTerminalWidth())
	rows := (len(entries) + cols - 1) / cols
	return rows
}

// LabelColors is the set of ANSI colors used for output labels.
var LabelColors = []string{
	"\033[36m", // cyan
	"\033[33m", // yellow
	"\033[35m", // magenta
	"\033[32m", // green
	"\033[34m", // blue
	"\033[31m", // red
	"\033[96m", // bright cyan
	"\033[93m", // bright yellow
	"\033[95m", // bright magenta
	"\033[92m", // bright green
}

const ColorReset = "\033[0m"

// FormatOutputLine formats a line of command output with a coloured label.
func FormatOutputLine(title string, line string, maxWidth int, colorIdx int) string {
	label := FormatLabel(title)
	color := LabelColors[colorIdx%len(LabelColors)]
	coloredLabel := color + label + ColorReset + " │ "
	// maxWidth calculation uses the visible width (without escape codes)
	visiblePrefix := label + " │ "
	maxLine := maxWidth - len(visiblePrefix)
	if maxLine < 10 {
		maxLine = 10
	}
	if len(line) > maxLine {
		line = line[:maxLine]
	}
	return coloredLabel + line
}


// Runner executes commands in parallel and tracks their statuses.
type Runner struct {
	Entries  []Entry
	Statuses []string

	StreamOut    bool
	OutputLines  []string
	outputWidth  int
	statusHeight int // rows for status entries + 1 for divider
	colorMap     map[string]int
	mu           sync.Mutex
	writer       io.Writer
	termHeight   int
	procs        []*os.Process // tracked processes for cleanup
	cancelled    bool
}

// NewRunner creates a Runner for the given entries.
func NewRunner(entries []Entry, writer io.Writer, streamOutput bool) *Runner {
	statuses := make([]string, len(entries))
	colorMap := make(map[string]int)
	for i := range statuses {
		statuses[i] = StatusRunning
		colorMap[entries[i].Title] = i
	}
	statusRows := StatusLines(entries)
	_, termH, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termH <= 0 {
		termH = 24
	}

	return &Runner{
		Entries:      entries,
		Statuses:     statuses,
		StreamOut:    streamOutput,
		outputWidth:  OutputWidth(),
		statusHeight: statusRows + 1, // entries + divider
		colorMap:     colorMap,
		writer:       writer,
		termHeight:   termH,
	}
}

// setupScrollRegion sets up the alternate screen and scroll region.
func (r *Runner) setupScrollRegion() {
	if r.writer == nil {
		return
	}
	// Switch to alternate screen buffer (preserves main scrollback)
	fmt.Fprintf(r.writer, "\033[?1049h")
	fmt.Fprintf(r.writer, "\033[?25l") // Hide cursor
	scrollEnd := r.termHeight - r.statusHeight
	if scrollEnd < 1 {
		scrollEnd = 1
	}
	// Set scroll region to top portion
	fmt.Fprintf(r.writer, "\033[1;%dr", scrollEnd)
	// Position cursor at bottom of scroll region
	fmt.Fprintf(r.writer, "\033[%d;1H", scrollEnd)
}

// resetScrollRegion restores the terminal to normal state.
func (r *Runner) resetScrollRegion() {
	if r.writer == nil {
		return
	}
	// Reset scroll region
	fmt.Fprintf(r.writer, "\033[r")
	// Show cursor
	fmt.Fprintf(r.writer, "\033[?25h")
	// Switch back to main screen buffer
	fmt.Fprintf(r.writer, "\033[?1049l")
}

// printFinalOutput reprints the captured output and final status to the main buffer.
func (r *Runner) printFinalOutput() {
	if r.writer == nil {
		return
	}
	termWidth := GetTerminalWidth()
	// Single blank line between command and output
	fmt.Fprintf(r.writer, "\n")
	// Reprint all captured output lines
	for _, line := range r.OutputLines {
		fmt.Fprintf(r.writer, "%s\n", line)
	}
	// Print divider
	fmt.Fprintf(r.writer, " %s\n", strings.Repeat("─", termWidth-2))
	// Print final status
	innerWidth := termWidth - 2
	cols := StatusColumns(innerWidth)
	availableWidth := innerWidth - (cols-1)*3
	colWidth := availableWidth / cols
	rows := (len(r.Entries) + cols - 1) / cols
	statusDisplayWidth := 2

	for row := 0; row < rows; row++ {
		fmt.Fprintf(r.writer, " ")
		first := true
		for col := 0; col < cols; col++ {
			idx := col*rows + row
			if idx >= len(r.Entries) {
				continue
			}
			if !first {
				fmt.Fprintf(r.writer, " │ ")
			}
			first = false
			e := r.Entries[idx]
			status := r.Statuses[idx]
			title := e.Title
			maxTitle := colWidth - statusDisplayWidth - 1
			if maxTitle < 5 {
				maxTitle = 5
			}
			if len(title) > maxTitle {
				title = title[:maxTitle]
			}
			padding := colWidth - len(title) - statusDisplayWidth
			if padding < 1 {
				padding = 1
			}
			fmt.Fprintf(r.writer, "%s%*s%s", title, padding, "", status)
		}
		fmt.Fprintf(r.writer, "\n")
	}
}

// renderStatus redraws the fixed status area (divider + entries) at the bottom.
func (r *Runner) renderStatus(frame int) {
	if r.writer == nil {
		return
	}
	var buf strings.Builder
	startRow := r.termHeight - r.statusHeight + 1
	termWidth := GetTerminalWidth()
	// Draw divider
	buf.WriteString(fmt.Sprintf("\033[%d;1H\033[K %s", startRow, strings.Repeat("─", termWidth-2)))
	// Draw status entries on subsequent rows
	innerWidth := termWidth - 2
	cols := StatusColumns(innerWidth)
	// Available width for entries after subtracting dividers between columns
	availableWidth := innerWidth - (cols-1)*3
	colWidth := availableWidth / cols
	rows := (len(r.Entries) + cols - 1) / cols
	statusDisplayWidth := 2

	for row := 0; row < rows; row++ {
		buf.WriteString(fmt.Sprintf("\033[%d;1H\033[K ", startRow+1+row))
		first := true
		for col := 0; col < cols; col++ {
			idx := col*rows + row
			if idx >= len(r.Entries) {
				continue
			}
			if !first {
				buf.WriteString(" │ ")
			}
			first = false
			e := r.Entries[idx]
			status := r.Statuses[idx]
			if status == StatusRunning {
				status = SpinnerFrames[frame%len(SpinnerFrames)]
			}
			title := e.Title
			maxTitle := colWidth - statusDisplayWidth - 1
			if maxTitle < 5 {
				maxTitle = 5
			}
			if len(title) > maxTitle {
				title = title[:maxTitle]
			}
			padding := colWidth - len(title) - statusDisplayWidth
			if padding < 1 {
				padding = 1
			}
			buf.WriteString(fmt.Sprintf("%s%*s%s", title, padding, "", status))
		}
	}
	// Move cursor back to the scroll region
	scrollEnd := r.termHeight - r.statusHeight
	if scrollEnd < 1 {
		scrollEnd = 1
	}
	buf.WriteString(fmt.Sprintf("\033[%d;1H", scrollEnd))
	fmt.Fprint(r.writer, buf.String())
}

// emitOutput prints a new output line in the scroll region.
func (r *Runner) emitOutput(title string, line string) {
	if r.writer == nil {
		return
	}
	colorIdx := r.colorMap[title]
	formatted := FormatOutputLine(title, line, r.outputWidth, colorIdx)
	r.OutputLines = append(r.OutputLines, formatted)
	scrollEnd := r.termHeight - r.statusHeight
	if scrollEnd < 1 {
		scrollEnd = 1
	}
	// Position at bottom of scroll region and print with newline to scroll
	fmt.Fprintf(r.writer, "\033[%d;1H\n\033[K %s", scrollEnd, formatted)
}

// Run executes all commands in parallel. Returns true if all succeeded.
func (r *Runner) Run() bool {
	var wg sync.WaitGroup

	// Handle SIGINT/SIGTERM for clean shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	r.mu.Lock()
	r.setupScrollRegion()
	r.renderStatus(0)
	r.mu.Unlock()

	// Spinner ticker
	done := make(chan struct{})
	go func() {
		frame := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				frame++
				r.mu.Lock()
				if !r.cancelled {
					r.renderStatus(frame)
				}
				r.mu.Unlock()
			}
		}
	}()

	// Signal handler goroutine
	go func() {
		select {
		case <-sigCh:
			r.mu.Lock()
			r.cancelled = true
			// Mark all running processes as killed
			for i, s := range r.Statuses {
				if s == StatusRunning {
					r.Statuses[i] = StatusKilled
				}
			}
			r.killAll()
			r.mu.Unlock()
		case <-done:
		}
		signal.Stop(sigCh)
	}()

	for i, entry := range r.Entries {
		wg.Add(1)
		go func(idx int, e Entry) {
			defer wg.Done()

			c := exec.Command("sh", "-c", e.Command)

			// Use a pty so commands emit colour output as if connected to a terminal
			ptmx, err := pty.Start(c)
			if err != nil {
				r.mu.Lock()
				r.setStatus(idx, StatusError)
				r.renderStatus(0)
				r.mu.Unlock()
				return
			}
			r.mu.Lock()
			r.trackProc(c.Process)
			r.mu.Unlock()

			// Read pty output (combined stdout+stderr from the pty)
			// Only emit if streaming stdout or always for stderr
			// With a pty, stdout and stderr are merged, so we always stream
			if r.StreamOut {
				r.streamLines(e.Title, ptmx)
			} else {
				// Even without -o, we want stderr. With a pty, stdout and
				// stderr are merged, so without -o we suppress all pty output
				// to avoid showing stdout. Use pipes for non-streaming mode.
				ptmx.Close()
				// Fall back to pipe mode for stderr-only
				c2 := exec.Command("sh", "-c", e.Command)
				c2.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				stderr, _ := c2.StderrPipe()
				if err2 := c2.Start(); err2 != nil {
					_ = c.Wait()
					r.mu.Lock()
					r.setStatus(idx, StatusError)
					r.renderStatus(0)
					r.mu.Unlock()
					return
				}
				r.mu.Lock()
				r.trackProc(c2.Process)
				r.mu.Unlock()
				r.streamLines(e.Title, stderr)
				err = c2.Wait()
				_ = c.Wait()
				r.mu.Lock()
				if err != nil {
					r.setStatus(idx, StatusError)
				} else {
					r.setStatus(idx, StatusDone)
				}
				r.renderStatus(0)
				r.mu.Unlock()
				return
			}

			err = c.Wait()
			ptmx.Close()
			r.mu.Lock()
			if err != nil {
				r.setStatus(idx, StatusError)
			} else {
				r.setStatus(idx, StatusDone)
			}
			r.renderStatus(0)
			r.mu.Unlock()
		}(i, entry)
	}

	wg.Wait()
	close(done)

	// Final render - switch back to main buffer and reprint output
	r.mu.Lock()
	r.resetScrollRegion()
	r.printFinalOutput()
	r.mu.Unlock()

	for _, s := range r.Statuses {
		if s == StatusError || s == StatusKilled {
			return false
		}
	}
	return true
}

func (r *Runner) streamLines(title string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		r.mu.Lock()
		r.emitOutput(title, line)
		r.mu.Unlock()
	}
}

// trackProc adds a process to the tracked list for cleanup.
func (r *Runner) trackProc(p *os.Process) {
	r.procs = append(r.procs, p)
}

// killAll sends SIGKILL to all tracked process groups.
func (r *Runner) killAll() {
	for _, p := range r.procs {
		if p != nil {
			// Kill the entire process group
			_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
		}
	}
}

// setStatus sets the status for an entry, but won't overwrite StatusKilled.
func (r *Runner) setStatus(idx int, status string) {
	if r.Statuses[idx] != StatusKilled {
		r.Statuses[idx] = status
	}
}
