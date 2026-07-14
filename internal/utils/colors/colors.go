// Package colors centralizes terminal color helpers for consistent output.
package colors

import "github.com/fatih/color"

var (
	Success = color.New(color.FgGreen).Sprint
	Failure = color.New(color.FgRed).Sprint
	Warning = color.New(color.FgYellow).Sprint
	Bold    = color.New(color.Bold).Sprint
	Faint   = color.New(color.Faint).Sprint
)
