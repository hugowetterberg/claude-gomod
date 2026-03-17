package main

import (
	"path"
	"regexp"
	"strings"
)

// searchMatch represents a single regexp match within a file.
type searchMatch struct {
	LineNum int      // 1-based line number of the matching line.
	Line    string   // The matching line text.
	Before  []string // Context lines before the match.
	After   []string // Context lines after the match.
}

// searchFileResult holds all matches found in a single file.
type searchFileResult struct {
	Path    string
	Matches []searchMatch
}

// fileMatches reports whether a file path passes the extension and
// glob filters. An empty extensions slice or empty glob matches
// everything.
func fileMatches(
	filePath string, extensions []string, glob string,
) bool {
	if len(extensions) > 0 {
		ext := path.Ext(filePath)
		found := false

		for _, e := range extensions {
			if strings.EqualFold(ext, e) {
				found = true

				break
			}
		}

		if !found {
			return false
		}
	}

	if glob != "" {
		matched, err := path.Match(glob, filePath)
		if err != nil || !matched {
			return false
		}
	}

	return true
}

// searchContent searches the given text content for lines matching
// the compiled regexp, returning matches with the requested number
// of context lines before and after each match.
func searchContent(
	re *regexp.Regexp, content string, before, after int,
) []searchMatch {
	lines := strings.Split(content, "\n")

	// Remove trailing empty line from final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var matches []searchMatch

	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}

		m := searchMatch{
			LineNum: i + 1,
			Line:    line,
		}

		// Collect before-context lines.
		start := max(0, i-before)
		if start < i {
			m.Before = make([]string, i-start)
			copy(m.Before, lines[start:i])
		}

		// Collect after-context lines.
		end := min(len(lines), i+after+1)
		if end > i+1 {
			m.After = make([]string, end-i-1)
			copy(m.After, lines[i+1:end])
		}

		matches = append(matches, m)
	}

	return matches
}
