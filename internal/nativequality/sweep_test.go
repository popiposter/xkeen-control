package nativequality

import (
	"reflect"
	"testing"
)

func TestSweepBatchCoverageAndFairRotation(t *testing.T) {
	for count := 2; count <= 18; count++ {
		sizes, err := batchSizes(count, 3)
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
	if _, err := batchSizes(19, 3); err == nil {
		t.Fatal("nineteen-node review admitted")
	}
	if sizes, _ := batchSizes(14, 3); !reflect.DeepEqual(sizes, []int{3, 3, 3, 3, 2}) {
		t.Fatal(sizes)
	}
}

func TestBatchSizesAreNearEqualAndNeverSingle(t *testing.T) {
	for _, tc := range []struct {
		count, batch int
		want         []int
	}{{12, 6, []int{6, 6}}, {13, 6, []int{5, 4, 4}}, {7, 6, []int{4, 3}}, {12, 3, []int{3, 3, 3, 3}}, {4, 3, []int{2, 2}}, {2, 6, []int{2}}} {
		got, err := batchSizes(tc.count, tc.batch)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("batchSizes(%d,%d) = %v %v", tc.count, tc.batch, got, err)
		}
	}
	for _, bad := range [][2]int{{1, 6}, {5, 1}, {19, 6}} {
		if _, err := batchSizes(bad[0], bad[1]); err == nil {
			t.Fatalf("batchSizes%v admitted", bad)
		}
	}
}
