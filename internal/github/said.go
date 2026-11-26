package github

import "strings"

// What the issues panel shows of the forge: its presets' names and what it
// answered. Both clients' panels show them, so they live here rather than
// in either client.

// FilterNames are the presets of the issues' list, or the pull requests'.
func FilterNames(pullRequests bool) []string {
	if pullRequests {
		names := make([]string, len(PRFilters))
		for i, f := range PRFilters {
			names[i] = PRFilterName(f)
		}
		return names
	}
	names := make([]string, len(Filters))
	for i, f := range Filters {
		names[i] = f.String()
	}
	return names
}

// ErrorText is what GitHub said, without the method it was said to: the
// client's error names the protocol method first ("github.issue: ...").
func ErrorText(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && strings.HasPrefix(msg, "github.") {
		msg = msg[i+2:]
	}
	return msg
}

// Said is what GitHub or a GitLab answered, for a line under the panel. A
// GitLab's answers name it already ("gitlab.com answered…", or the token it
// wants); what gh said is said to be GitHub's.
func Said(err error) string {
	msg := ErrorText(err)
	if strings.Contains(msg, "GitLab") || strings.Contains(msg, "gitlab") {
		return msg
	}
	return "GitHub said: " + msg
}
