# choir

A CLI utility that runs multiple shell commands in parallel with live status updates.

## Installation

```bash
go build -o choir .
```

## Usage

```bash
choir [flags] <command1> [command2] ...
```

### Flags

| Flag | Description |
|------|-------------|
| `-o`, `--output` | Stream stdout from commands (stderr is always shown) |
| `-t`, `--template` | Command template with `%%` placeholder for piped input |

### Custom Titles

Assign a custom title to any command using the `title::command` syntax:

```bash
choir "Build::make build" "Test::go test ./..." "Lint::golangci-lint run"
```

Titles must be unique. If no title is provided, the command itself is used as the title.

### Output Streaming

Stderr is always displayed above the status list. Use `-o` to also stream stdout:

```bash
choir -o "Build::make build" "Test::go test -v ./..."
```

Output lines are labelled with colour-coded command titles and displayed in real-time order.

### Templates

Pipe input lines to generate parallel commands from a template. Each line replaces `%%`:

```bash
cat servers.txt | choir --template "ssh %% 'uptime'"
```

```bash
echo -e "fmt\nvet\ntest" | choir -t "go %% ./..."
```

Each piped line becomes both the command title and the `%%` substitution.

Templates can be combined with other flags:

```bash
ls *.json | choir -o -t "jq '.' %%"
```

## Display

```
Build                │ compiled successfully
Test                 │ FAIL: TestFoo
─────────────────────────────────────────────────────────
Build                                             ✅️
Test                                              ❌
Lint                                              ⠹
```

- Commands are listed with their status on the right at column 50
- Long titles wrap at 45 characters without splitting words
- Status indicators: spinner (running), ✅️ (success), ❌ (failure)
- Output labels are padded to 20 characters and colour-coded per command
- The display overwrites in place — scrollback only shows the final state

## Exit Code

Exits `0` if all commands succeed, `1` if any command fails.

## Development

```bash
go test -v -timeout 10s
```
