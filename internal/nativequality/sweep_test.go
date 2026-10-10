package nativequality

import (
	"reflect"
	"testing"
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
	if sizes, _ := batchSizes(14); !reflect.DeepEqual(sizes, []int{3, 3, 3, 3, 2}) {
		t.Fatal(sizes)
	}
}
