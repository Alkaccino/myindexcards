package main

import (
	"html"
	"html/template"
	"strings"
)

// FormatCardText converts card text with simple markup to HTML:
//   - ```...``` → code block
//   - `...` → inline code
//   - Lines starting with • or - → list items
//   - |...|...| → table
//   - ![alt](url) → image
//   - \n → <br>
func FormatCardText(s string) template.HTML {
	s = html.EscapeString(s)

	// Process code blocks (``` ... ```)
	var result strings.Builder
	for {
		start := strings.Index(s, "```")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+3:], "```")
		if end == -1 {
			break
		}
		end += start + 3

		before := s[:start]
		code := s[start+3 : end]
		s = s[end+3:]

		// Process lines FIRST (tables, lists, images), inline code applied inside
		result.WriteString(formatLines(before))
		code = strings.TrimPrefix(code, "\n")
		code = strings.TrimSuffix(code, "\n")
		result.WriteString(`<pre class="code-block"><code>`)
		result.WriteString(code)
		result.WriteString(`</code></pre>`)
	}
	result.WriteString(formatLines(s))

	return template.HTML(result.String())
}

// inlineCode handles backtick inline code, **bold**, and ==highlight== within a text fragment
func inlineCode(s string) string {
	s = inlineMarkup(s, "**", `<span class="card-bold">`, `</span>`)
	s = inlineMarkup(s, "==", `<span class="card-highlight">`, `</span>`)
	s = inlineBacktick(s)
	return s
}

// inlineBacktick wraps `...` in <code> tags
func inlineBacktick(s string) string {
	var result strings.Builder
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1

		result.WriteString(s[:start])
		result.WriteString(`<code class="code-inline">`)
		result.WriteString(s[start+1 : end])
		result.WriteString(`</code>`)
		s = s[end+1:]
	}
	result.WriteString(s)
	return result.String()
}

// inlineMarkup wraps delimited text (e.g. **bold** or ==highlight==) in HTML tags
func inlineMarkup(s, delim, openTag, closeTag string) string {
	var result strings.Builder
	for {
		start := strings.Index(s, delim)
		if start == -1 {
			break
		}
		end := strings.Index(s[start+len(delim):], delim)
		if end == -1 {
			break
		}
		end += start + len(delim)

		result.WriteString(s[:start])
		result.WriteString(openTag)
		result.WriteString(s[start+len(delim) : end])
		result.WriteString(closeTag)
		s = s[end+len(delim):]
	}
	result.WriteString(s)
	return result.String()
}

// formatLines handles tables, bullet points, images, and line breaks.
// Inline code is processed AFTER line-level structure recognition.
func formatLines(s string) string {
	lines := strings.Split(s, "\n")
	var result strings.Builder
	inList := false
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Image: ![alt](url)
		if strings.HasPrefix(trimmed, "![") {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			alt, url := parseImage(trimmed)
			if url != "" {
				result.WriteString(`<div class="card-image"><img src="`)
				result.WriteString(url)
				result.WriteString(`" alt="`)
				result.WriteString(alt)
				result.WriteString(`"></div>`)
				i++
				continue
			}
		}

		// Horizontal divider: ---
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			result.WriteString(`<hr class="card-divider">`)
			i++
			continue
		}

		// Table: collect all contiguous table rows, then render as a block
		if isTableLine(trimmed) {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			var tableRows []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if !isTableLine(t) {
					break
				}
				tableRows = append(tableRows, t)
				i++
			}
			result.WriteString(renderTable(tableRows))
			continue
		}

		// Bullet list
		if strings.HasPrefix(trimmed, "• ") || strings.HasPrefix(trimmed, "- ") {
			if !inList {
				result.WriteString(`<ul class="card-list-fmt">`)
				inList = true
			}
			var content string
			if strings.HasPrefix(trimmed, "• ") {
				content = trimmed[len("• "):]
			} else {
				content = trimmed[2:]
			}
			result.WriteString("<li>")
			result.WriteString(inlineCode(content))
			result.WriteString("</li>")
		} else {
			if inList {
				result.WriteString("</ul>")
				inList = false
			}
			result.WriteString(inlineCode(line))
			if i < len(lines)-1 {
				result.WriteString("<br>")
			}
		}
		i++
	}
	if inList {
		result.WriteString("</ul>")
	}
	return result.String()
}

// isTableLine checks if a line looks like a table row or separator
func isTableLine(s string) bool {
	return strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") && len(s) > 2
}

// renderTable renders a collected group of table rows with proper thead/tbody and th support
func renderTable(rows []string) string {
	// Find separator row index
	sepIdx := -1
	for i, row := range rows {
		if isTableSeparator(row) {
			sepIdx = i
			break
		}
	}

	var result strings.Builder
	result.WriteString(`<table class="card-table">`)

	hasHeader := sepIdx > 0
	firstHeaderCellEmpty := false

	if hasHeader {
		// Render header rows (before separator) as <thead> with <th>
		result.WriteString("<thead>")
		for i := 0; i < sepIdx; i++ {
			cells := parseTableRow(rows[i])
			if i == 0 && len(cells) > 0 && strings.TrimSpace(cells[0]) == "" {
				firstHeaderCellEmpty = true
			}
			result.WriteString("<tr>")
			for _, cell := range cells {
				result.WriteString("<th>")
				result.WriteString(inlineCode(strings.TrimSpace(cell)))
				result.WriteString("</th>")
			}
			result.WriteString("</tr>")
		}
		result.WriteString("</thead>")
	}

	// Render body rows
	result.WriteString("<tbody>")
	startIdx := 0
	if hasHeader {
		startIdx = sepIdx + 1
	}
	for i := startIdx; i < len(rows); i++ {
		if isTableSeparator(rows[i]) {
			continue
		}
		cells := parseTableRow(rows[i])
		result.WriteString("<tr>")
		for j, cell := range cells {
			if j == 0 && firstHeaderCellEmpty {
				result.WriteString("<th>")
				result.WriteString(inlineCode(strings.TrimSpace(cell)))
				result.WriteString("</th>")
			} else {
				result.WriteString("<td>")
				result.WriteString(inlineCode(strings.TrimSpace(cell)))
				result.WriteString("</td>")
			}
		}
		result.WriteString("</tr>")
	}
	result.WriteString("</tbody></table>")

	return result.String()
}

// parseImage extracts alt text and URL from ![alt](url)
func parseImage(s string) (alt, url string) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "![") {
		return "", ""
	}
	closeBracket := strings.Index(s, "](")
	if closeBracket == -1 {
		return "", ""
	}
	alt = s[2:closeBracket]
	rest := s[closeBracket+2:]
	closeParen := strings.Index(rest, ")")
	if closeParen == -1 {
		return "", ""
	}
	url = rest[:closeParen]
	return alt, url
}

// parseTableRow splits a table row into cells
func parseTableRow(s string) []string {
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	return strings.Split(s, "|")
}

// isTableSeparator checks if a row is a separator like |---|---|
func isTableSeparator(s string) bool {
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	for _, cell := range strings.Split(s, "|") {
		cell = strings.TrimSpace(cell)
		cleaned := strings.ReplaceAll(cell, "-", "")
		cleaned = strings.ReplaceAll(cleaned, ":", "")
		if cleaned != "" {
			return false
		}
	}
	return true
}
