//go:build linux && mipsle

package validationbudget

import "time"

const Candidate = 120 * time.Second
const Transaction = 375 * time.Second
