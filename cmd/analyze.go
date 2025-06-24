package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [project-name]",
	Short: "Analyze a Claude project",
	Long:  "Analyze a specific Claude project by listing all files in the project directory.\n\nFor project names starting with '-', use: clan analyze -- -project-name",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		analyzeProject(args[0])
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
}

func analyzeProject(projectName string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		return
	}

	projectDir := filepath.Join(homeDir, ".claude", "projects", projectName)

	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		fmt.Printf("Project '%s' not found\n", projectName)
		return
	}

	// Find all JSON/JSONL files in the project directory
	files, err := findProjectFiles(projectDir)
	if err != nil {
		fmt.Printf("Error finding project files: %v\n", err)
		return
	}

	if len(files) == 0 {
		fmt.Printf("No JSON/JSONL files found in project '%s'\n", projectName)
		return
	}

	// Load data from the first file
	entries, err := loadDataFromFile(files[0])
	if err != nil {
		fmt.Printf("Error loading project data: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Printf("No conversation entries found in project '%s'\n", projectName)
		return
	}

	// Start Bubble Tea program
	model := newModel(entries, projectName, projectDir, files, 0)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

type ConversationEntry struct {
	ParentUuid  *string `json:"parentUuid,omitempty"`
	IsSidechain *bool   `json:"isSidechain,omitempty"`
	UserType    *string `json:"userType,omitempty"`
	Cwd         *string `json:"cwd,omitempty"`
	SessionId   *string `json:"sessionId,omitempty"`
	Version     *string `json:"version,omitempty"`
	Message     Message `json:"message"`
	RequestId   *string `json:"requestId,omitempty"`
	Type        string  `json:"type"`
	Uuid        string  `json:"uuid"`
	Timestamp   string  `json:"timestamp"`
}

type Message struct {
	Id           *string         `json:"id,omitempty"`
	Type         *string         `json:"type,omitempty"`
	Role         string          `json:"role"`
	Model        *string         `json:"model,omitempty"`
	Content      json.RawMessage `json:"content"`
	StopReason   *string         `json:"stop_reason,omitempty"`
	StopSequence *string         `json:"stop_sequence,omitempty"`
	Usage        *Usage          `json:"usage,omitempty"`
}

type Content struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	Id    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type Usage struct {
	InputTokens              int    `json:"input_tokens"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	ServiceTier              string `json:"service_tier"`
}

// Bubble Tea Model
type model struct {
	entries        []ConversationEntry
	projectName    string
	cursor         int
	viewportHeight int
	viewportWidth  int
	scrollOffset   int
	projectDir     string
	files          []string
	currentFileIdx int
	errorMessage   string
}

func newModel(entries []ConversationEntry, projectName, projectDir string, files []string, currentFileIdx int) model {
	return model{
		entries:        entries,
		projectName:    projectName,
		cursor:         0,
		viewportWidth:  80, // Default width
		viewportHeight: 24, // Default height
		scrollOffset:   0,
		projectDir:     projectDir,
		files:          files,
		currentFileIdx: currentFileIdx,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewportHeight = msg.Height
		m.viewportWidth = msg.Width
		return m, nil

	case tea.KeyMsg:
		// Clear error message on any key press
		m.errorMessage = ""

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 && len(m.entries) > 0 {
				m.cursor--
				// Adjust scroll if cursor goes above visible area
				if m.cursor < m.scrollOffset {
					m.scrollOffset = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.entries)-1 && len(m.entries) > 0 {
				m.cursor++
				// Adjust scroll if cursor goes below visible area
				visibleHeight := m.getListHeight()
				if m.cursor >= m.scrollOffset+visibleHeight {
					m.scrollOffset = m.cursor - visibleHeight + 1
				}
			}

		case "n":
			// Next file
			if m.currentFileIdx < len(m.files)-1 {
				return m.loadFile(m.currentFileIdx + 1), nil
			}

		case "p":
			// Previous file
			if m.currentFileIdx > 0 {
				return m.loadFile(m.currentFileIdx - 1), nil
			}

		case "left":
			// Previous user prompt
			if len(m.entries) > 0 {
				newPosition := m.findPreviousUserPrompt()
				if newPosition != m.cursor && newPosition >= 0 && newPosition < len(m.entries) {
					m.cursor = newPosition
					// Adjust scroll if cursor goes above visible area
					if m.cursor < m.scrollOffset {
						m.scrollOffset = m.cursor
					}
				}
			}

		case "right":
			// Next user prompt
			if len(m.entries) > 0 {
				newPosition := m.findNextUserPrompt()
				if newPosition != m.cursor && newPosition >= 0 && newPosition < len(m.entries) {
					m.cursor = newPosition
					// Adjust scroll if cursor goes below visible area
					visibleHeight := m.getListHeight()
					if m.cursor >= m.scrollOffset+visibleHeight {
						m.scrollOffset = m.cursor - visibleHeight + 1
					}
				}
			}

		case "e":
			// Export user prompts
			err := m.exportUserPrompts()
			if err != nil {
				// Could add error handling here if needed
				// For now, silently continue
			}
		}
	}

	return m, nil
}

func (m model) getListHeight() int {
	// Calculate available height for the list
	// Account for: header (3 lines) + status line (1 line) + borders + padding
	availableHeight := m.viewportHeight - 6
	if availableHeight < 1 {
		return 5 // Minimum height
	}
	return availableHeight
}

func (m model) loadFile(fileIdx int) model {
	if fileIdx < 0 || fileIdx >= len(m.files) {
		m.errorMessage = "Invalid file index"
		return m
	}

	entries, err := loadDataFromFile(m.files[fileIdx])
	if err != nil {
		m.errorMessage = fmt.Sprintf("Error loading file: %v", err)
		return m
	}

	// Create new model with the loaded data and reset positions
	return model{
		entries:        entries,
		projectName:    m.projectName,
		cursor:         0, // Reset to first entry
		viewportHeight: m.viewportHeight,
		viewportWidth:  m.viewportWidth,
		scrollOffset:   0, // Reset scroll to top
		projectDir:     m.projectDir,
		files:          m.files,
		currentFileIdx: fileIdx,
		errorMessage:   "", // Clear error on successful load
	}
}

func (m model) View() string {
	// Handle zero width gracefully
	if m.viewportWidth == 0 {
		return "Loading..."
	}

	// Calculate pane widths
	leftWidth := int(float64(m.viewportWidth) * 0.3)
	rightWidth := m.viewportWidth - leftWidth - 1 // -1 for border

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Width(m.viewportWidth)

	currentFile := "unknown"
	if m.currentFileIdx < len(m.files) {
		currentFile = filepath.Base(m.files[m.currentFileIdx])
	}

	header := headerStyle.Render(fmt.Sprintf("Claude Project: %s | File: %s (%d/%d) | Entries: %d",
		m.projectName, currentFile, m.currentFileIdx+1, len(m.files), len(m.entries)))

	// Left pane - Entry list
	leftPane := m.renderLeftPane(leftWidth)

	// Right pane - Details
	rightPane := m.renderRightPane(rightWidth)

	// Combine panes
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftPane,
		rightPane,
	)

	// Status line at bottom
	var status string
	if m.errorMessage != "" {
		// Show error message in red
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Background(lipgloss.Color("235")).
			Width(m.viewportWidth).
			PaddingLeft(1).
			PaddingRight(1)
		status = errorStyle.Render(m.errorMessage + " (press any key to continue)")
	} else {
		// Show normal status
		statusStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Background(lipgloss.Color("235")).
			Width(m.viewportWidth).
			PaddingLeft(1).
			PaddingRight(1)

		statusText := fmt.Sprintf("Keys: ↑/↓,j/k: navigate | ←/→: prev/next user prompt | n/p: next/prev file | e: export prompts | q: quit | [U]=User [A]=Assistant [T]=Tools | Entry: %d/%d",
			m.cursor+1, len(m.entries))

		status = statusStyle.Render(statusText)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		content,
		status,
	)
}

func (m model) renderLeftPane(width int) string {
	height := m.getListHeight()

	leftStyle := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	if len(m.entries) == 0 {
		return leftStyle.Render("No entries")
	}

	var listItems []string

	// Calculate visible range
	start := m.scrollOffset
	end := start + height
	if end > len(m.entries) {
		end = len(m.entries)
	}

	// Calculate available width for content (account for padding and borders)
	contentWidth := width - 4 // 2 for padding + 2 for borders

	// Render only visible items
	for i := start; i < end; i++ {
		entry := m.entries[i]
		shortSummary := m.getShortSummary(entry)

		// Visual indicators with consistent width
		var indicator string
		var style lipgloss.Style

		if entry.Message.Role == "user" {
			indicator = "[U]"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // Blue
		} else {
			// Check if tools were used
			hasTools := m.hasTools(entry)
			if hasTools {
				indicator = "[T]"
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // Orange
			} else {
				indicator = "[A]"
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("46")) // Green
			}
		}

		// Highlight current selection with strong visual indication
		if i == m.cursor {
			style = style.Bold(true).
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230"))
		}

		// Create consistent-width item with proper text width calculation
		item := fmt.Sprintf("%s %s", indicator, shortSummary)

		// Calculate actual display width (accounting for multi-byte characters)
		displayWidth := utf8.RuneCountInString(item)

		// Ensure consistent width by padding or truncating
		if displayWidth > contentWidth {
			// Truncate while preserving UTF-8 boundaries
			truncated := ""
			count := 0
			for _, r := range item {
				if count >= contentWidth-3 {
					break
				}
				truncated += string(r)
				count++
			}
			item = truncated + "..."
		} else {
			// Pad with spaces to ensure consistent width
			padding := contentWidth - displayWidth
			item = item + strings.Repeat(" ", padding)
		}

		listItems = append(listItems, style.Width(contentWidth).Render(item))
	}

	// Fill remaining space if needed
	emptyStyle := lipgloss.NewStyle().Width(contentWidth)
	for len(listItems) < height {
		listItems = append(listItems, emptyStyle.Render(""))
	}

	content := strings.Join(listItems, "\n")
	return leftStyle.Render(content)
}

func (m model) renderRightPane(width int) string {
	rightStyle := lipgloss.NewStyle().
		Width(width).
		Padding(1, 2)

	if len(m.entries) == 0 {
		return rightStyle.Render("No entries found")
	}

	if m.cursor >= len(m.entries) || m.cursor < 0 {
		return rightStyle.Render("")
	}

	entry := m.entries[m.cursor]
	details := m.renderEntryDetails(entry)

	if details == "" {
		return rightStyle.Render("No details available for this entry")
	}

	return rightStyle.Render(details)
}

func (m model) exportUserPrompts() error {
	// Create filename based on project name
	filename := fmt.Sprintf("%s.md", m.projectName)

	// Collect all relevant messages from all files
	var allMessages []MessageEntry

	// Process all files in the project
	for _, filePath := range m.files {
		entries, err := loadDataFromFile(filePath)
		if err != nil {
			continue // Skip files that can't be loaded
		}

		for _, entry := range entries {
			var textContent string
			var isQuestion bool

			if entry.Message.Role == "user" {
				// User messages - extract text content
				if err := json.Unmarshal(entry.Message.Content, &textContent); err == nil {
					// Simple string content
					if strings.TrimSpace(textContent) != "" {
						allMessages = append(allMessages, MessageEntry{
							Content:   textContent,
							IsUser:    true,
							Timestamp: entry.Timestamp,
						})
					}
				} else {
					// Array content - extract text parts
					var contentArray []Content
					if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
						var textParts []string
						for _, content := range contentArray {
							if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
								textParts = append(textParts, content.Text)
							}
						}
						if len(textParts) > 0 {
							allMessages = append(allMessages, MessageEntry{
								Content:   strings.Join(textParts, "\n\n"),
								IsUser:    true,
								Timestamp: entry.Timestamp,
							})
						}
					}
				}
			} else if entry.Message.Role == "assistant" {
				// Assistant messages - look for questions
				var contentArray []Content
				if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
					var textParts []string
					for _, content := range contentArray {
						if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
							text := strings.TrimSpace(content.Text)
							// Check if it's likely a question
							if strings.Contains(text, "?") ||
								strings.Contains(strings.ToLower(text), "could you") ||
								strings.Contains(strings.ToLower(text), "can you") ||
								strings.Contains(strings.ToLower(text), "would you") ||
								strings.Contains(strings.ToLower(text), "please") ||
								strings.Contains(strings.ToLower(text), "which") ||
								strings.Contains(strings.ToLower(text), "what") ||
								strings.Contains(strings.ToLower(text), "how") ||
								strings.Contains(strings.ToLower(text), "when") ||
								strings.Contains(strings.ToLower(text), "where") ||
								strings.Contains(strings.ToLower(text), "why") {
								textParts = append(textParts, text)
								isQuestion = true
							}
						}
					}
					if len(textParts) > 0 && isQuestion {
						allMessages = append(allMessages, MessageEntry{
							Content:   strings.Join(textParts, "\n\n"),
							IsUser:    false,
							Timestamp: entry.Timestamp,
						})
					}
				}
			}
		}
	}

	if len(allMessages) == 0 {
		return fmt.Errorf("no user prompts or assistant questions found to export")
	}

	// Generate nicely formatted markdown content
	var md strings.Builder

	// Add header
	md.WriteString(fmt.Sprintf("# 💬 Conversation Prompts & Questions\n"))
	md.WriteString(fmt.Sprintf("**Project:** %s\n\n", m.projectName))
	md.WriteString("---\n\n")

	for i, message := range allMessages {
		content := strings.TrimSpace(message.Content)

		if message.IsUser {
			md.WriteString("## 👤 User Prompt\n\n")
		} else {
			md.WriteString("## 🤖 Assistant\n\n")
		}

		md.WriteString(content)
		md.WriteString("\n\n")

		// Add separator between messages (except for the last one)
		if i < len(allMessages)-1 {
			md.WriteString("---\n\n")
		}
	}

	// Write to file
	return os.WriteFile(filename, []byte(md.String()), 0o644)
}

type MessageEntry struct {
	Content   string
	IsUser    bool
	Timestamp string
}

func (m model) getShortSummary(entry ConversationEntry) string {
	// Calculate available space for summary (total width - indicator - spaces)
	leftWidth := int(float64(m.viewportWidth) * 0.3)
	contentWidth := leftWidth - 4           // Account for padding and borders
	availableForSummary := contentWidth - 4 // Account for "[X] " indicator

	if availableForSummary < 5 {
		availableForSummary = 10 // Minimum reasonable space
	}

	// Extract content text
	var text string
	var contentStr string
	if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
		text = contentStr
	} else {
		// Handle array content
		var contentArray []Content
		if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
			var textParts []string
			for _, content := range contentArray {
				if content.Type == "text" && content.Text != "" {
					textParts = append(textParts, content.Text)
				}
			}
			text = strings.Join(textParts, " ")
		}
	}

	if text == "" {
		return "No content"
	}

	// Clean up the text (remove extra whitespace)
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	// Remove multiple spaces
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}

	// Truncate to fit available space, breaking at word boundaries when possible
	if utf8.RuneCountInString(text) <= availableForSummary {
		return text
	}

	// Find the best place to cut
	words := strings.Fields(text)
	result := ""

	for _, word := range words {
		testResult := result
		if testResult != "" {
			testResult += " "
		}
		testResult += word

		if utf8.RuneCountInString(testResult) > availableForSummary-3 { // Reserve space for "..."
			break
		}
		result = testResult
	}

	if result == "" {
		// If even the first word is too long, truncate it
		runes := []rune(text)
		if len(runes) > availableForSummary-3 {
			result = string(runes[:availableForSummary-3])
		} else {
			result = text
		}
	}

	return result
}

func (m model) hasTools(entry ConversationEntry) bool {
	var contentArray []Content
	if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
		for _, content := range contentArray {
			if content.Type == "tool_use" {
				return true
			}
		}
	}
	return false
}

func (m model) isNonEmptyUserPrompt(entry ConversationEntry) bool {
	if entry.Message.Role != "user" {
		return false
	}

	// Extract content text
	var text string
	var contentStr string
	if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
		text = contentStr
	} else {
		// Handle array content
		var contentArray []Content
		if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
			var textParts []string
			for _, content := range contentArray {
				if content.Type == "text" && content.Text != "" {
					textParts = append(textParts, content.Text)
				}
			}
			text = strings.Join(textParts, " ")
		}
	}

	// Check if text content is meaningful (not just whitespace)
	return strings.TrimSpace(text) != ""
}

func (m model) findNextUserPrompt() int {
	for i := m.cursor + 1; i < len(m.entries); i++ {
		if m.isNonEmptyUserPrompt(m.entries[i]) {
			return i
		}
	}
	return m.cursor // No next non-empty user prompt found, stay at current position
}

func (m model) findPreviousUserPrompt() int {
	for i := m.cursor - 1; i >= 0; i-- {
		if m.isNonEmptyUserPrompt(m.entries[i]) {
			return i
		}
	}
	return m.cursor // No previous non-empty user prompt found, stay at current position
}

func (m model) renderEntryDetails(entry ConversationEntry) string {
	var lines []string

	// Title
	if entry.Message.Role == "user" {
		lines = append(lines, "👤 USER MESSAGE")
	} else {
		model := "Assistant"
		if entry.Message.Model != nil {
			model = *entry.Message.Model
		}
		lines = append(lines, fmt.Sprintf("🤖 %s", strings.ToUpper(model)))
	}

	lines = append(lines, "")

	// Entry Metadata
	lines = append(lines, "📋 Entry Metadata:")
	lines = append(lines, fmt.Sprintf("  UUID: %s", entry.Uuid))
	lines = append(lines, fmt.Sprintf("  Type: %s", entry.Type))
	lines = append(lines, fmt.Sprintf("  Timestamp: %s", entry.Timestamp))

	if entry.RequestId != nil {
		lines = append(lines, fmt.Sprintf("  Request ID: %s", *entry.RequestId))
	}
	if entry.ParentUuid != nil {
		lines = append(lines, fmt.Sprintf("  Parent UUID: %s", *entry.ParentUuid))
	}
	if entry.IsSidechain != nil {
		lines = append(lines, fmt.Sprintf("  Is Sidechain: %t", *entry.IsSidechain))
	}
	if entry.UserType != nil {
		lines = append(lines, fmt.Sprintf("  User Type: %s", *entry.UserType))
	}
	if entry.Cwd != nil {
		lines = append(lines, fmt.Sprintf("  Working Directory: %s", *entry.Cwd))
	}
	if entry.Version != nil {
		lines = append(lines, fmt.Sprintf("  Version: %s", *entry.Version))
	}
	if entry.SessionId != nil {
		lines = append(lines, fmt.Sprintf("  Session ID: %s", *entry.SessionId))
	}

	lines = append(lines, "")

	// Message Metadata
	lines = append(lines, "📨 Message Metadata:")
	lines = append(lines, fmt.Sprintf("  Role: %s", entry.Message.Role))
	if entry.Message.Id != nil {
		lines = append(lines, fmt.Sprintf("  Message ID: %s", *entry.Message.Id))
	}
	if entry.Message.Type != nil {
		lines = append(lines, fmt.Sprintf("  Message Type: %s", *entry.Message.Type))
	}
	if entry.Message.StopReason != nil {
		lines = append(lines, fmt.Sprintf("  Stop Reason: %s", *entry.Message.StopReason))
	}
	if entry.Message.StopSequence != nil {
		lines = append(lines, fmt.Sprintf("  Stop Sequence: %s", *entry.Message.StopSequence))
	}

	lines = append(lines, "")

	// Parse and display content
	var textContent []string
	var toolDetails []Content

	var contentStr string
	if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
		textContent = append(textContent, contentStr)
	} else {
		var contentArray []Content
		if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
			for _, content := range contentArray {
				switch content.Type {
				case "text":
					if content.Text != "" {
						textContent = append(textContent, content.Text)
					}
				case "tool_use":
					if content.Name != "" {
						toolDetails = append(toolDetails, content)
					}
				}
			}
		}
	}

	// Content
	lines = append(lines, "💬 Content:")
	if len(textContent) > 0 {
		content := strings.Join(textContent, " ")
		// Show full content without truncation for better readability
		lines = append(lines, fmt.Sprintf("  %s", content))
	} else {
		lines = append(lines, "  (No text content)")
	}

	lines = append(lines, "")

	// Tools
	if len(toolDetails) > 0 {
		lines = append(lines, "🛠️  Tools Used:")
		for _, tool := range toolDetails {
			toolHeader := fmt.Sprintf("  • %s", tool.Name)
			if tool.Id != "" {
				toolHeader += fmt.Sprintf(" (ID: %s)", tool.Id)
			}
			lines = append(lines, toolHeader)
			if tool.Input != nil {
				switch tool.Name {
				case "TodoWrite":
					if todos, ok := tool.Input["todos"].([]interface{}); ok {
						lines = append(lines, fmt.Sprintf("    Todo items (%d):", len(todos)))
						for i, todoItem := range todos {
							if todo, ok := todoItem.(map[string]interface{}); ok {
								content := getStringValue(todo, "content")
								status := getStringValue(todo, "status")
								priority := getStringValue(todo, "priority")
								lines = append(lines, fmt.Sprintf("      %d. [%s/%s] %s", i+1, status, priority, content))
							}
						}
					}
				case "Bash":
					if command, ok := tool.Input["command"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Command: %s", command))
					}
					if desc, ok := tool.Input["description"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Description: %s", desc))
					}
				case "Read":
					if path, ok := tool.Input["file_path"].(string); ok {
						lines = append(lines, fmt.Sprintf("    File: %s", path))
					}
					if offset, ok := tool.Input["offset"].(float64); ok {
						lines = append(lines, fmt.Sprintf("    Starting at line: %.0f", offset))
					}
					if limit, ok := tool.Input["limit"].(float64); ok {
						lines = append(lines, fmt.Sprintf("    Lines to read: %.0f", limit))
					}
				case "Edit":
					if path, ok := tool.Input["file_path"].(string); ok {
						lines = append(lines, fmt.Sprintf("    File: %s", path))
					}
					if oldStr, ok := tool.Input["old_string"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Replace: %s", oldStr))
					}
					if newStr, ok := tool.Input["new_string"].(string); ok {
						lines = append(lines, fmt.Sprintf("    With: %s", newStr))
					}
				case "Write":
					if path, ok := tool.Input["file_path"].(string); ok {
						lines = append(lines, fmt.Sprintf("    File: %s", path))
					}
					if content, ok := tool.Input["content"].(string); ok {
						contentLines := strings.Split(content, "\n")
						lines = append(lines, fmt.Sprintf("    Content: %d lines, %d characters", len(contentLines), len(content)))
					}
				case "Glob":
					if pattern, ok := tool.Input["pattern"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Pattern: %s", pattern))
					}
					if path, ok := tool.Input["path"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Search path: %s", path))
					}
				case "Grep":
					if pattern, ok := tool.Input["pattern"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Search pattern: %s", pattern))
					}
					if include, ok := tool.Input["include"].(string); ok {
						lines = append(lines, fmt.Sprintf("    File filter: %s", include))
					}
					if path, ok := tool.Input["path"].(string); ok {
						lines = append(lines, fmt.Sprintf("    Search path: %s", path))
					}
				case "MultiEdit":
					if path, ok := tool.Input["file_path"].(string); ok {
						lines = append(lines, fmt.Sprintf("    File: %s", path))
					}
					if edits, ok := tool.Input["edits"].([]interface{}); ok {
						lines = append(lines, fmt.Sprintf("    Edits (%d):", len(edits)))
						for i, editItem := range edits {
							if edit, ok := editItem.(map[string]interface{}); ok {
								oldStr := getStringValue(edit, "old_string")
								newStr := getStringValue(edit, "new_string")
								lines = append(lines, fmt.Sprintf("      %d. Replace: %s", i+1, oldStr))
								lines = append(lines, fmt.Sprintf("         With: %s", newStr))
							}
						}
					}
				default:
					// Generic handling for unknown tools
					for key, value := range tool.Input {
						if str, ok := value.(string); ok {
							lines = append(lines, fmt.Sprintf("    %s: %s", key, str))
						} else if arr, ok := value.([]interface{}); ok {
							lines = append(lines, fmt.Sprintf("    %s (%d items):", key, len(arr)))
							for i, item := range arr {
								lines = append(lines, fmt.Sprintf("      %d. %v", i+1, item))
							}
						} else {
							lines = append(lines, fmt.Sprintf("    %s: %v", key, value))
						}
					}
				}
			}
		}
		lines = append(lines, "")
	}

	// Enhanced Usage Information
	if entry.Message.Usage != nil {
		lines = append(lines, "📊 Token Usage:")
		lines = append(lines, fmt.Sprintf("  Input Tokens: %d", entry.Message.Usage.InputTokens))
		lines = append(lines, fmt.Sprintf("  Output Tokens: %d", entry.Message.Usage.OutputTokens))

		if entry.Message.Usage.CacheCreationInputTokens > 0 {
			lines = append(lines, fmt.Sprintf("  Cache Creation Tokens: %d", entry.Message.Usage.CacheCreationInputTokens))
		}
		if entry.Message.Usage.CacheReadInputTokens > 0 {
			lines = append(lines, fmt.Sprintf("  Cache Read Tokens: %d", entry.Message.Usage.CacheReadInputTokens))
		}

		totalTokens := entry.Message.Usage.InputTokens + entry.Message.Usage.OutputTokens
		lines = append(lines, fmt.Sprintf("  Total Tokens: %d", totalTokens))

		if entry.Message.Usage.ServiceTier != "" {
			lines = append(lines, fmt.Sprintf("  Service Tier: %s", entry.Message.Usage.ServiceTier))
		}
	}

	return strings.Join(lines, "\n")
}

func (m model) formatDetailedToolInput(tool Content) string {
	switch tool.Name {
	case "TodoWrite":
		return m.formatTodoWriteInput(tool.Input)
	case "Bash":
		return m.formatBashInput(tool.Input)
	case "Read":
		return m.formatReadInput(tool.Input)
	case "Edit":
		return m.formatEditInput(tool.Input)
	case "Write":
		return m.formatWriteInput(tool.Input)
	case "Glob":
		return m.formatGlobInput(tool.Input)
	case "Grep":
		return m.formatGrepInput(tool.Input)
	case "MultiEdit":
		return m.formatMultiEditInput(tool.Input)
	default:
		return m.formatGenericInput(tool.Input)
	}
}

func (m model) formatTodoWriteInput(input map[string]interface{}) string {
	if todos, ok := input["todos"].([]interface{}); ok {
		var result strings.Builder
		result.WriteString(fmt.Sprintf("Todo items (%d):\n", len(todos)))
		for i, todoItem := range todos {
			if todo, ok := todoItem.(map[string]interface{}); ok {
				content := getStringValue(todo, "content")
				status := getStringValue(todo, "status")
				priority := getStringValue(todo, "priority")
				result.WriteString(fmt.Sprintf("  %d. [%s/%s] %s\n", i+1, status, priority, content))
			}
		}
		return result.String()
	}
	return ""
}

func (m model) formatBashInput(input map[string]interface{}) string {
	var result strings.Builder
	if command, ok := input["command"].(string); ok {
		result.WriteString(fmt.Sprintf("Command: %s\n", command))
	}
	if desc, ok := input["description"].(string); ok {
		result.WriteString(fmt.Sprintf("Description: %s\n", desc))
	}
	if timeout, ok := input["timeout"].(float64); ok {
		result.WriteString(fmt.Sprintf("Timeout: %.0fms\n", timeout))
	}
	return result.String()
}

func (m model) formatReadInput(input map[string]interface{}) string {
	var result strings.Builder
	if path, ok := input["file_path"].(string); ok {
		result.WriteString(fmt.Sprintf("File: %s\n", path))
	}
	if offset, ok := input["offset"].(float64); ok {
		result.WriteString(fmt.Sprintf("Starting at line: %.0f\n", offset))
	}
	if limit, ok := input["limit"].(float64); ok {
		result.WriteString(fmt.Sprintf("Lines to read: %.0f\n", limit))
	}
	return result.String()
}

func (m model) formatEditInput(input map[string]interface{}) string {
	var result strings.Builder
	if path, ok := input["file_path"].(string); ok {
		result.WriteString(fmt.Sprintf("File: %s\n", path))
	}
	if oldStr, ok := input["old_string"].(string); ok {
		result.WriteString(fmt.Sprintf("Replace: %s\n", truncateText(oldStr, 100)))
	}
	if newStr, ok := input["new_string"].(string); ok {
		result.WriteString(fmt.Sprintf("With: %s\n", truncateText(newStr, 100)))
	}
	if replaceAll, ok := input["replace_all"].(bool); ok && replaceAll {
		result.WriteString("Replace all occurrences: true\n")
	}
	return result.String()
}

func (m model) formatWriteInput(input map[string]interface{}) string {
	var result strings.Builder
	if path, ok := input["file_path"].(string); ok {
		result.WriteString(fmt.Sprintf("File: %s\n", path))
	}
	if content, ok := input["content"].(string); ok {
		lines := strings.Split(content, "\n")
		chars := len(content)
		result.WriteString(fmt.Sprintf("Content: %d lines, %d characters\n", len(lines), chars))
		if len(lines) > 0 {
			preview := truncateText(lines[0], 80)
			result.WriteString(fmt.Sprintf("Preview: %s\n", preview))
		}
	}
	return result.String()
}

func (m model) formatGlobInput(input map[string]interface{}) string {
	var result strings.Builder
	if pattern, ok := input["pattern"].(string); ok {
		result.WriteString(fmt.Sprintf("Pattern: %s\n", pattern))
	}
	if path, ok := input["path"].(string); ok {
		result.WriteString(fmt.Sprintf("Search path: %s\n", path))
	}
	return result.String()
}

func (m model) formatGrepInput(input map[string]interface{}) string {
	var result strings.Builder
	if pattern, ok := input["pattern"].(string); ok {
		result.WriteString(fmt.Sprintf("Search pattern: %s\n", pattern))
	}
	if include, ok := input["include"].(string); ok {
		result.WriteString(fmt.Sprintf("File filter: %s\n", include))
	}
	if path, ok := input["path"].(string); ok {
		result.WriteString(fmt.Sprintf("Search path: %s\n", path))
	}
	return result.String()
}

func (m model) formatMultiEditInput(input map[string]interface{}) string {
	var result strings.Builder
	if path, ok := input["file_path"].(string); ok {
		result.WriteString(fmt.Sprintf("File: %s\n", path))
	}
	if edits, ok := input["edits"].([]interface{}); ok {
		result.WriteString(fmt.Sprintf("Number of edits: %d\n", len(edits)))
	}
	return result.String()
}

func (m model) formatGenericInput(input map[string]interface{}) string {
	var result strings.Builder
	for key, value := range input {
		if str, ok := value.(string); ok {
			result.WriteString(fmt.Sprintf("%s: %s\n", key, truncateText(str, 100)))
		} else {
			result.WriteString(fmt.Sprintf("%s: %v\n", key, value))
		}
	}
	return result.String()
}

func getStringValue(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func extractContentSummary(entry ConversationEntry) string {
	var contentStr string
	if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
		return truncateText(contentStr, 80)
	} else {
		var contentArray []Content
		if err := json.Unmarshal(entry.Message.Content, &contentArray); err == nil {
			var text []string
			var tools []string
			for _, content := range contentArray {
				if content.Type == "text" && content.Text != "" {
					text = append(text, content.Text)
				} else if content.Type == "tool_use" && content.Name != "" {
					tools = append(tools, content.Name)
				}
			}
			result := strings.Join(text, " ")
			if len(tools) > 0 {
				result += fmt.Sprintf(" [Tools: %s]", strings.Join(tools, ", "))
			}
			return truncateText(result, 80)
		}
	}
	return "No content"
}

func loadProjectData(projectDir string) ([]ConversationEntry, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	var jsonFile string
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".jsonl")) {
			jsonFile = filepath.Join(projectDir, entry.Name())
			break
		}
	}

	if jsonFile == "" {
		return nil, fmt.Errorf("no JSON/JSONL files found in project directory")
	}

	file, err := os.Open(jsonFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var conversationEntries []ConversationEntry
	scanner := bufio.NewScanner(file)

	// Increase buffer size to handle large conversation entries (up to 10MB per line)
	buf := make([]byte, 0, 64*1024)   // Start with 64KB initial buffer
	scanner.Buffer(buf, 10*1024*1024) // Allow up to 10MB max token size

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry ConversationEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip invalid lines but continue processing
			continue
		}

		conversationEntries = append(conversationEntries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return conversationEntries, nil
}

func findProjectFiles(projectDir string) ([]string, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	type fileInfo struct {
		path    string
		modTime int64
	}

	var files []fileInfo
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".jsonl")) {
			filePath := filepath.Join(projectDir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, fileInfo{
				path:    filePath,
				modTime: info.ModTime().Unix(),
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime < files[j].modTime
	})

	var jsonFiles []string
	for _, file := range files {
		jsonFiles = append(jsonFiles, file.path)
	}

	return jsonFiles, nil
}

func loadDataFromFile(filePath string) ([]ConversationEntry, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var conversationEntries []ConversationEntry
	scanner := bufio.NewScanner(file)

	// Increase buffer size to handle large conversation entries (up to 10MB per line)
	buf := make([]byte, 0, 64*1024)   // Start with 64KB initial buffer
	scanner.Buffer(buf, 10*1024*1024) // Allow up to 10MB max token size

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry ConversationEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip invalid lines but continue processing
			continue
		}

		conversationEntries = append(conversationEntries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return conversationEntries, nil
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
