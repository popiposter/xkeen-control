package validationbudget

import (
	"runtime"
	"testing"
	"time"
)

func TestBuiltTargetBudget(t *testing.T) {
	candidate, total := 45*time.Second, 300*time.Second
	if runtime.GOOS == "linux" && runtime.GOARCH == "mipsle" {
		candidate, total = 120*time.Second, 375*time.Second
	}
	if Candidate != candidate || Transaction != total || Transaction < Candidate+240*time.Second+15*time.Second {
		t.Fatalf("incorrect target budget: validation %s, transaction %s", Candidate, Transaction)
	}
	if PreparationMargin != 5*time.Second || HTTPMargin != 30*time.Second {
		t.Fatal("changed transport/preparation margins")
	}
}
