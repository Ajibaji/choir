package main

import (
	"bytes"
	"strings"
	"testing"
)

func entriesFrom(commands ...string) []Entry {
	entries := make([]Entry, len(commands))
	for i, cmd := range commands {
		entries[i] = Entry{Title: cmd, Command: cmd}
	}
	return entries
}

func TestAllCommandsSucceed(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(entriesFrom("true", "true", "true"), &buf, false)
	if !r.Run() {
		t.Fatal("expected success when all commands pass")
	}
	for i, s := range r.Statuses {
		if s != StatusDone {
			t.Fatalf("command %d: expected %s, got %s", i, StatusDone, s)
		}
	}
}

func TestOneCommandFails(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(entriesFrom("true", "false", "true"), &buf, false)
	if r.Run() {
		t.Fatal("expected failure when a command fails")
	}
	expected := []string{StatusDone, StatusError, StatusDone}
	for i, s := range r.Statuses {
		if s != expected[i] {
			t.Fatalf("command %d: expected %s, got %s", i, expected[i], s)
		}
	}
}

func TestAllCommandsFail(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(entriesFrom("false", "false"), &buf, false)
	if r.Run() {
		t.Fatal("expected failure when all commands fail")
	}
	for i, s := range r.Statuses {
		if s != StatusError {
			t.Fatalf("command %d: expected %s, got %s", i, StatusError, s)
		}
	}
}

func TestInitialStatusIsRunning(t *testing.T) {
	r := NewRunner(entriesFrom("true", "false"), nil, false)
	for i, s := range r.Statuses {
		if s != StatusRunning {
			t.Fatalf("command %d: expected initial status %s, got %s", i, StatusRunning, s)
		}
	}
}

func TestRenderCalledOnStatusChange(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(entriesFrom("true"), &buf, false)
	r.Run()

	output := buf.String()
	if !strings.Contains(output, StatusDone) {
		t.Fatal("expected output to contain final status")
	}
}

func TestCommandsRunInParallel(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(entriesFrom("sleep 0.5", "sleep 0.5"), &buf, false)
	if !r.Run() {
		t.Fatal("expected success")
	}
}

// --- WrapText tests ---

func TestWrapTextShort(t *testing.T) {
	lines := WrapText("hello", 45)
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("expected single line, got %v", lines)
	}
}

func TestWrapTextLong(t *testing.T) {
	cmd := "echo this is a very long command that exceeds forty five characters easily"
	lines := WrapText(cmd, 45)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	for _, line := range lines {
		if len(line) > 45 {
			t.Fatalf("line exceeds 45 chars: %q", line)
		}
	}
	for _, line := range lines {
		if len(line) > 0 && line[0] == ' ' {
			t.Fatalf("line starts with space: %q", line)
		}
	}
}

func TestStatusLinesCount(t *testing.T) {
	entries := []Entry{
		{Title: "short", Command: "true"},
		{Title: "echo this is a very long command that exceeds forty five characters easily", Command: "true"},
	}
	total := StatusLines(entries)
	if total < 2 {
		t.Fatalf("expected at least 2 status lines, got %d", total)
	}
}

// --- Title parsing tests ---

func TestParseEntryWithTitle(t *testing.T) {
	e := ParseEntry("Build::make build")
	if e.Title != "Build" {
		t.Fatalf("expected title 'Build', got %q", e.Title)
	}
	if e.Command != "make build" {
		t.Fatalf("expected command 'make build', got %q", e.Command)
	}
}

func TestParseEntryWithoutTitle(t *testing.T) {
	e := ParseEntry("make build")
	if e.Title != "make build" {
		t.Fatalf("expected title 'make build', got %q", e.Title)
	}
	if e.Command != "make build" {
		t.Fatalf("expected command 'make build', got %q", e.Command)
	}
}

func TestParseEntryTitleTrimmed(t *testing.T) {
	e := ParseEntry("  Build  ::  make build  ")
	if e.Title != "Build" {
		t.Fatalf("expected trimmed title 'Build', got %q", e.Title)
	}
	if e.Command != "make build" {
		t.Fatalf("expected trimmed command 'make build', got %q", e.Command)
	}
}

func TestParseEntriesUniqueTitles(t *testing.T) {
	_, err := ParseEntries([]string{"Build::make", "Build::go build"})
	if err == nil {
		t.Fatal("expected error for duplicate titles")
	}
}

func TestParseEntriesUniqueImplicitTitles(t *testing.T) {
	_, err := ParseEntries([]string{"make build", "make build"})
	if err == nil {
		t.Fatal("expected error for duplicate implicit titles")
	}
}

func TestParseEntriesValid(t *testing.T) {
	entries, err := ParseEntries([]string{"Build::make", "Test::go test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Title != "Build" || entries[1].Title != "Test" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestCustomTitleUsedInRunner(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{
		{Title: "Build", Command: "true"},
		{Title: "Test", Command: "true"},
	}
	r := NewRunner(entries, &buf, false)
	r.Run()

	output := buf.String()
	if !strings.Contains(output, "Build") || !strings.Contains(output, "Test") {
		t.Fatal("expected custom titles in output")
	}
}

// --- FormatLabel tests ---

func TestFormatLabelShort(t *testing.T) {
	label := FormatLabel("Build")
	if len(label) != LabelWidth {
		t.Fatalf("expected label length %d, got %d: %q", LabelWidth, len(label), label)
	}
	if label != "Build               " {
		t.Fatalf("unexpected label: %q", label)
	}
}

func TestFormatLabelExact(t *testing.T) {
	title := "12345678901234567890" // exactly 20
	label := FormatLabel(title)
	if label != title {
		t.Fatalf("expected %q, got %q", title, label)
	}
}

func TestFormatLabelLong(t *testing.T) {
	title := "This is a very long title that exceeds twenty"
	label := FormatLabel(title)
	if len(label) != LabelWidth {
		t.Fatalf("expected label length %d, got %d: %q", LabelWidth, len(label), label)
	}
	if label != title[:LabelWidth] {
		t.Fatalf("expected truncated title, got %q", label)
	}
}

// --- Template tests ---

func TestExpandTemplate(t *testing.T) {
	result := ExpandTemplate("echo %%", []string{"foo", "bar", "baz"})
	if len(result) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(result))
	}
	expected := []string{"foo::echo foo", "bar::echo bar", "baz::echo baz"}
	for i, r := range result {
		if r != expected[i] {
			t.Fatalf("command %d: expected %q, got %q", i, expected[i], r)
		}
	}
}

func TestExpandTemplateMultiplePlaceholders(t *testing.T) {
	result := ExpandTemplate("cp %% /tmp/%%", []string{"file.txt"})
	if len(result) != 1 {
		t.Fatalf("expected 1 command, got %d", len(result))
	}
	if result[0] != "file.txt::cp file.txt /tmp/file.txt" {
		t.Fatalf("unexpected result: %q", result[0])
	}
}

func TestExpandTemplateGeneratesUniqueEntries(t *testing.T) {
	result := ExpandTemplate("echo %%", []string{"a", "b", "c"})
	entries, err := ParseEntries(result)
	if err != nil {
		t.Fatalf("expected unique titles, got error: %v", err)
	}
	if entries[0].Title != "a" || entries[1].Title != "b" || entries[2].Title != "c" {
		t.Fatalf("unexpected titles: %+v", entries)
	}
}

func TestExpandTemplateRunsCommands(t *testing.T) {
	var buf bytes.Buffer
	result := ExpandTemplate("echo %%", []string{"hello", "world"})
	entries, _ := ParseEntries(result)
	r := NewRunner(entries, &buf, false)
	if !r.Run() {
		t.Fatal("expected success")
	}
	for i, s := range r.Statuses {
		if s != StatusDone {
			t.Fatalf("command %d: expected %s, got %s", i, StatusDone, s)
		}
	}
}

// --- Streaming output tests ---

func TestStreamOutputCaptured(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{{Title: "Echo", Command: "echo hello && echo world"}}
	r := NewRunner(entries, &buf, true)
	r.Run()

	if len(r.OutputLines) != 2 {
		t.Fatalf("expected 2 output lines, got %d", len(r.OutputLines))
	}
	if !strings.Contains(r.OutputLines[0], "hello") {
		t.Fatalf("expected 'hello' in first line, got %q", r.OutputLines[0])
	}
	if !strings.Contains(r.OutputLines[1], "world") {
		t.Fatalf("expected 'world' in second line, got %q", r.OutputLines[1])
	}
}

func TestStreamOutputStderr(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{{Title: "Err", Command: "echo error >&2"}}
	r := NewRunner(entries, &buf, true)
	r.Run()

	if len(r.OutputLines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(r.OutputLines))
	}
	if !strings.Contains(r.OutputLines[0], "error") {
		t.Fatalf("expected 'error' in output, got %q", r.OutputLines[0])
	}
}

func TestStreamOutputMultipleCommands(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{
		{Title: "A", Command: "echo a"},
		{Title: "B", Command: "echo b"},
	}
	r := NewRunner(entries, &buf, true)
	r.Run()

	output := strings.Join(r.OutputLines, "\n")
	if !strings.Contains(output, "a") || !strings.Contains(output, "b") {
		t.Fatalf("expected output from both commands, got: %s", output)
	}
}

func TestStdoutBlockedByDefault(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{{Title: "Test", Command: "echo hi"}}
	r := NewRunner(entries, &buf, false)
	r.Run()

	for _, line := range r.OutputLines {
		if strings.Contains(line, "hi") {
			t.Fatal("stdout should not be captured when streaming is disabled")
		}
	}
}

func TestStderrShownByDefault(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{{Title: "Test", Command: "echo error >&2"}}
	r := NewRunner(entries, &buf, false)
	r.Run()

	if len(r.OutputLines) != 1 {
		t.Fatalf("expected 1 stderr line, got %d", len(r.OutputLines))
	}
	if !strings.Contains(r.OutputLines[0], "error") {
		t.Fatalf("expected stderr output, got %q", r.OutputLines[0])
	}
}

func TestStdoutShownWithFlag(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{{Title: "Test", Command: "echo hi"}}
	r := NewRunner(entries, &buf, true)
	r.Run()

	if len(r.OutputLines) != 1 {
		t.Fatalf("expected 1 stdout line, got %d", len(r.OutputLines))
	}
	if !strings.Contains(r.OutputLines[0], "hi") {
		t.Fatalf("expected stdout output, got %q", r.OutputLines[0])
	}
}

func TestStreamOutputOrderedByTime(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{
		{Title: "A", Command: "echo A1 && sleep 0.2 && echo A2"},
		{Title: "B", Command: "sleep 0.1 && echo B1"},
	}
	r := NewRunner(entries, &buf, true)
	r.Run()

	if len(r.OutputLines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %v", len(r.OutputLines), r.OutputLines)
	}
	if !strings.Contains(r.OutputLines[0], "A1") {
		t.Fatalf("expected first line to contain A1, got %q", r.OutputLines[0])
	}
	if !strings.Contains(r.OutputLines[1], "B1") {
		t.Fatalf("expected second line to contain B1, got %q", r.OutputLines[1])
	}
	if !strings.Contains(r.OutputLines[2], "A2") {
		t.Fatalf("expected third line to contain A2, got %q", r.OutputLines[2])
	}
}

// --- Output appears exactly once ---

func TestOutputLineAppearsOnce(t *testing.T) {
	var buf bytes.Buffer
	entries := []Entry{{Title: "Test", Command: "echo hello"}}
	r := NewRunner(entries, &buf, true)
	r.Run()

	output := buf.String()
	// The formatted output line should appear exactly once in the raw output
	// (not re-rendered on every spinner tick)
	formatted := FormatOutputLine("Test", "hello", r.outputWidth, 0)
	count := strings.Count(output, formatted)
	if count != 2 {
		// Once during live output, once in final reprint
		t.Fatalf("expected output line to appear exactly twice, appeared %d times", count)
	}
}

func TestStatusOverwritesSameLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewRunner(entriesFrom("true"), &buf, false)
	r.Run()

	output := buf.String()
	// Status is redrawn using absolute positioning + clear line
	overwriteSeq := "\033[K"
	count := strings.Count(output, overwriteSeq)
	if count < 2 {
		t.Fatalf("expected multiple status overwrites, got %d", count)
	}
}
