# README.md

## Notes from the Vibe Coder

What you're looking at here is the product of pure vibe coding - every line of Go, every UI component, and yes, even this README was crafted entirely by Claude. I found myself needing this tool to help with another project and thought, "why not just ask Claude to build it?"

My contribution was minimal: pick Go as the language, and somewhere along the way mention I'd heard good things about Bubbletea (though I'd never actually used it). That's it. Claude took the reins from there, choosing Cobra for the CLI framework without even glancing at my own [clap](https://github.com/fred1268/go-clap) library that was sitting right there in my GitHub.

The speed was honestly impressive. Watching a functional CLI emerge from nothing in what felt like minutes was a bit surreal. Sure, Claude hit some bumps - particularly with the left panel which took an oddly stubborn 4 or 5 iterations to behave properly - but it self-corrected with minimal hand-holding from me.

Am I in love with the resulting code architecture? Not particularly. There's a part of me that wants to circle back and have Claude refactor it into something cleaner, if I can find the time and motivation.

But here's the thing: it works, it was fast to build, and the process was genuinely pleasant. Total cost? $18.07 using claude-sonnet-4-20250514. The flip side? I still haven't learned Bubbletea myself. If this becomes the new normal - me directing Claude like a programming project manager - I wonder if I'll still find the same joy in coding that I do today.

---

*This file also provides guidance to Claude Code (claude.ai/code) when working with code in this repository.*

## Project Overview

`clan` is a Go CLI tool for parsing and analyzing Claude conversation logs stored in JSON/JSONL format. It provides an interactive terminal UI for browsing conversation entries, viewing message details, and analyzing tool usage patterns.

## Build and Development Commands

```bash
# Build the application
go build -o clan

# Run without building
go run main.go

# Build and install to GOPATH/bin
go install

# Run tests
go test ./...

# Get dependencies
go mod download

# Update dependencies
go mod tidy
```

## Architecture Overview

The application follows a standard Cobra CLI pattern with subcommands:

- **main.go**: Entry point that calls `cmd.Execute()`
- **cmd/root.go**: Root command definition
- **cmd/list.go**: Lists available Claude projects from `~/.claude/projects`
- **cmd/analyze.go**: Core functionality for parsing and displaying conversation data

### Key Components

**JSON Parsing Strategy**: The application handles two different message formats:
- User messages: `message.content` is a string
- Assistant messages: `message.content` is an array of Content objects

The `Message` struct uses `json.RawMessage` for the `Content` field to handle both formats, with parsing logic that attempts string unmarshaling first, then falls back to array unmarshaling.

**Interactive UI**: Built with Bubble Tea framework, featuring:
- Split-pane interface (entry list on left, details on right)
- Keyboard navigation (↑/↓, j/k for navigation, n/p for file switching)
- Color-coded entry types: [U] User (blue), [A] Assistant (green), [T] Tools (orange)

**Data Structures**:
- `ConversationEntry`: Top-level wrapper with metadata (timestamp, session, UUID)
- `Message`: Core message data with role, content, and optional fields (model, usage, etc.)
- `Content`: Individual content blocks within assistant messages (text, tool_use)
- `Usage`: Token consumption tracking

### Project Structure Expectations

The tool expects Claude projects to be organized as:
```
~/.claude/projects/
├── project-name-1/
│   ├── conversation.jsonl
│   └── other-files.json
└── project-name-2/
    └── data.jsonl
```

### Handling Special Cases

**Project Names with Dashes**: Use `--` separator for project names starting with `-`:
```bash
clan analyze -- -project-name
```

**Multiple Files**: The UI supports navigating between multiple JSON/JSONL files within a project using 'n' (next) and 'p' (previous) keys.

**Tool Usage Analysis**: The application specifically recognizes and formats details for common Claude tools (Bash, Read, Edit, Write, Glob, Grep, TodoWrite, MultiEdit) with specialized display logic for their input parameters.

