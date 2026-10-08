package nodes

import (
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/validationbudget"
)

func TestValidationBudgetDefaultsAndExplicitOverrides(t *testing.T) {
	b := (TransactionBudget{}).normalized()
	if b.CandidateValidation != validationbudget.Candidate || b.Total != validationbudget.Transaction || b.Activation != 120*time.Second || b.Rollback != 120*time.Second {
		t.Fatalf("wrong default budget: %+v", b)
	}
	explicit := TransactionBudget{time.Second, 2 * time.Second, 3 * time.Second, 7 * time.Second}
	if explicit.normalized() != explicit {
		t.Fatal("explicit phase budgets changed")
	}
}
