package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]

	// Parse flags
	streamOutput := false
	template := ""
	var commands []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--output", "-o":
			streamOutput = true
		case "--template", "-t":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --template requires a value\n")
				os.Exit(1)
			}
			i++
			template = args[i]
		default:
			commands = append(commands, args[i])
		}
	}

	// If template is set, read stdin lines and generate commands
	if template != "" {
		lines, err := readStdinLines()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
		if len(lines) == 0 {
			fmt.Fprintf(os.Stderr, "Error: --template requires piped input\n")
			os.Exit(1)
		}
		commands = append(commands, ExpandTemplate(template, lines)...)
	}

	if len(commands) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: choir [-o|--output] [-t|--template <template>] <[title::]command1> ...\n")
		fmt.Fprintf(os.Stderr, "  Template: use '%%%%' as placeholder for piped input lines\n")
		fmt.Fprintf(os.Stderr, "  Template: use 'command%%%%' to transform the value (e.g. basename%%%%)\n")
		os.Exit(1)
	}

	entries, err := ParseEntries(commands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	r := NewRunner(entries, os.Stdout, streamOutput)

	if !r.Run() {
		os.Exit(1)
	}
}

func readStdinLines() ([]string, error) {
	info, _ := os.Stdin.Stat()
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, nil
	}
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}
