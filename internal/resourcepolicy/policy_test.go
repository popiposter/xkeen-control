package resourcepolicy

import (
	"testing"
	"time"
)

func TestReviewLimitsCoverTheLadderWorstCase(t *testing.T) {
	// MIPS, standard ARM64 and low-memory ARM64 (constrained limits).
	for _, profile := range []Profile{ForPlatform("mipsle", 254472), ForPlatform("arm64", 1024*1024), ForPlatform("arm64", 200*1024)} {
		if !profile.Automatic {
			t.Fatalf("%s: every profile shares the automatic policy", profile.Name)
		}
		review, batch := profile.Review(), profile.Comparison(false)
		worst := int64(0)
		for _, n := range append(append([]int64(nil), batch.Download...), batch.Upload...) {
			worst += n
		}
		batches := (review.Candidates + review.BatchSize - 1) / review.BatchSize
		if review.Bytes < int64(review.Candidates)*worst || review.BatchSize > batch.Candidates || int64(review.BatchSize)*worst > batch.Bytes {
			t.Fatalf("%s: review bytes %d below %d candidates × %d", profile.Name, review.Bytes, review.Candidates, worst)
		}
		if review.Wall < time.Duration(batches)*batch.Wall()+time.Duration(batches-1)*review.Pause {
			t.Fatalf("%s: review wall %s below batches", profile.Name, review.Wall)
		}
	}
}

func TestReviewForScalesBudgetsWithCandidates(t *testing.T) {
	constrained := Profile{Constrained: true}
	if got := constrained.ReviewFor(18); got != constrained.Review() || got.Bytes != 108*MiB || got.Wall != 16*time.Minute {
		t.Fatal("constrained default", got)
	}
	if got := constrained.ReviewFor(6); got.Candidates != 6 || got.Bytes != 36*MiB || got.Wall != 2*90*time.Second+time.Minute+2*time.Minute {
		t.Fatal("constrained six", got)
	}
	if constrained.Review().Repeat != 0 {
		t.Fatal("constrained profiles have no second pass")
	}
	// Standard: 88 MiB per node, 300 s per batch of six, plus a second pass
	// over the best eight.
	standard := Profile{}
	if got := standard.ReviewFor(18); got.Repeat != 8 || got.Bytes != 26*88*MiB || got.Wall != 5*300*time.Second+2*time.Minute {
		t.Fatal("standard default", got)
	}
	if got := standard.ReviewFor(7); got.Candidates != 7 || got.Repeat != 7 || got.Bytes != 14*88*MiB || got.Wall != 4*300*time.Second+2*time.Minute {
		t.Fatal("standard seven", got)
	}
	// The whole default review stays inside the 30-minute evidence window.
	if standard.Review().Wall > 30*time.Minute {
		t.Fatal("review longer than the evidence window")
	}
	if standard.ReviewFor(99).Candidates != 18 || standard.ReviewFor(0).Candidates != 2 {
		t.Fatal("breadth not clamped")
	}
}
