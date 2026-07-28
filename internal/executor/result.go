// Package executor runs submitted commands with bounded output capture.
package executor

// Result contains the process execution outcome.
type Result struct {
	Stdout     string
	Stderr     string
	ExitStatus *int
	Truncated  bool
	Error      string
	TimedOut   bool
}
