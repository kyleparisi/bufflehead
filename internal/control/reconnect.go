package control

import "strings"

// reconnectStepLabels maps machine step names to human-readable descriptions.
var reconnectStepLabels = map[string]string{
	"cancel_queries":      "Cancel running queries",
	"close_db":            "Close database connection",
	"stop_tunnel":         "Stop SSM tunnel",
	"refresh_credentials": "Refresh AWS credentials",
	"start_tunnel":        "Start SSM tunnel",
	"connect_db":          "Connect to database",
}

// FormatReconnectSteps renders a reconnect outcome as a readable, multi-line
// summary suitable for display in an error panel.
//
// friendly, if non-nil, is called with a step's raw error message and may return
// a nicer message plus a bool indicating it recognized the error (e.g. expired
// login). When it reports true, the friendly text is shown (indented) instead of
// the raw error.
func FormatReconnectSteps(name string, ok bool, steps []ReconnectStep, friendly func(string) (string, bool)) string {
	var b strings.Builder
	if ok {
		b.WriteString("Reconnected " + name + " successfully.\n\n")
	} else {
		b.WriteString("Reconnecting " + name + " failed.\n\n")
	}
	for _, s := range steps {
		label := reconnectStepLabels[s.Step]
		if label == "" {
			label = s.Step
		}
		mark := "[ok]"
		if !s.OK {
			mark = "[x] "
		}
		b.WriteString(mark + " " + label)
		if s.Error != "" {
			msg := s.Error
			if friendly != nil {
				if f, ok := friendly(s.Error); ok {
					msg = f
				}
			}
			b.WriteString("\n    " + strings.ReplaceAll(msg, "\n", "\n    "))
		}
		b.WriteString("\n")
	}
	if !ok {
		b.WriteString("\nClick the connection and refresh again to retry.")
	}
	return b.String()
}
