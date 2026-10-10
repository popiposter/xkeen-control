package resourcepolicy

import (
	"testing"
	"time"
)

func TestReviewLimitsCoverTheLadderWorstCase(t *testing.T) {
	for _, profile := range []Profile{ForPlatform("mipsle", 254472), ForPlatform("arm64", 1024*1024)} {
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
