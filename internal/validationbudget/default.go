//go:build !linux || !mipsle

// Package validationbudget defines bounded validation defaults for the built target.
package validationbudget

import "time"

const Candidate = 45 * time.Second
const Transaction = 300 * time.Second
