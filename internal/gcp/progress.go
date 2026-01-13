package gcp

import "strings"

// ProgressEvent describes a progress update emitted during resource discovery.
type ProgressEvent struct {
	// Key identifies the logical task (e.g. "region:us-central1").
	Key string
	// Message contains the human-readable update to display.
	Message string
	// Err is set when the task finishes with an error.
	Err error
	// Done reports whether the task has completed (successfully or with error).
	Done bool
	// Info marks the completion as informational (neither success nor failure).
	Info bool
}

// ProgressCallback receives progress updates for long running discovery tasks.
type ProgressCallback func(ProgressEvent)

// FormatProgressKey normalises labels for spinners to keep output concise.
func FormatProgressKey(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return label
	}

	if idx := strings.IndexRune(label, ' '); idx > 0 {
		return label[:idx]
	}

	return label
}

func progressUpdate(cb ProgressCallback, key, message string) {
	if cb == nil {
		return
	}

	cb(ProgressEvent{Key: key, Message: message})
}

func progressSuccess(cb ProgressCallback, key, message string) {
	if cb == nil {
		return
	}

	cb(ProgressEvent{Key: key, Message: message, Done: true})
}

func progressFailure(cb ProgressCallback, key, message string, err error) {
	if cb == nil {
		return
	}

	cb(ProgressEvent{Key: key, Message: message, Err: err, Done: true})
}
