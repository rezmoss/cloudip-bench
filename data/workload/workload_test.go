package workload

import (
	"net/netip"
	"testing"

	"github.com/rezmoss/cloudip-bench/data/snapshot"
)

// samples are drawn with replacement; log how many are distinct.
func TestDistinct(t *testing.T) {
	db, err := snapshot.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range All(db) {
		seen := map[netip.Addr]bool{}
		for _, a := range s.Addrs {
			seen[a] = true
		}
		t.Logf("%-8s %5d distinct of %d", s.Name, len(seen), len(s.Addrs))
	}
}
