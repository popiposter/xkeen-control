package nativequality

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func TestSweepBatchCoverageAndFairRotation(t *testing.T) {
	for count := 2; count <= 18; count++ {
		sizes, err := batchSizes(count)
		if err != nil || len(sizes) > 6 {
			t.Fatalf("count %d: %v %v", count, sizes, err)
		}
		total := 0
		for _, size := range sizes {
			if size < 2 || size > 3 {
				t.Fatalf("invalid batch size %d", size)
			}
			total += size
		}
		if total != count {
			t.Fatalf("count %d covered %d", count, total)
		}
	}
	if _, err := batchSizes(19); err == nil {
		t.Fatal("nineteen-node review admitted")
	}
	var inputs []c1.AdaptiveCandidateInput
	for i := 0; i < 14; i++ {
		inputs = append(inputs, c1.AdaptiveCandidateInput{Tag: fmt.Sprintf("proxy-%02d", i)})
	}
	if sizes, _ := batchSizes(14); !reflect.DeepEqual(sizes, []int{3, 3, 3, 3, 2}) {
		t.Fatal(sizes)
	}
	for cursor := 0; cursor < 14; cursor++ {
		rotated := rotateCandidates(inputs, cursor)
		if rotated[0].Tag != inputs[cursor].Tag || len(rotated) != len(inputs) {
			t.Fatalf("unfair cursor %d", cursor)
		}
		seen := map[string]bool{}
		for _, candidate := range rotated {
			if seen[candidate.Tag] {
				t.Fatal("duplicate candidate")
			}
			seen[candidate.Tag] = true
		}
	}
}
