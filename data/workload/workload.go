// Package workload: seeded synthetic address sets, same generators as
// go-cloudip v0.0.20 bench_test.go.
package workload

import (
	"math/rand/v2"
	"net/netip"

	"github.com/rezmoss/cloudip-bench/data/snapshot"
)

// N addrs per set; power of 2 so loops wrap with a mask.
const N = 1 << 16

// Hits: random addr in a uniformly picked row (dup prefixes picked more
// often). All hit.
func Hits(db *snapshot.DB, v6 bool) []netip.Addr {
	rng := rand.New(rand.NewPCG(3, 4))
	out := make([]netip.Addr, 0, N)
	for len(out) < N {
		p := netip.MustParsePrefix(db.Ranges[rng.IntN(len(db.Ranges))].CIDR).Masked()
		if p.Addr().Is6() != v6 {
			continue
		}
		b := p.Addr().AsSlice()
		for i := p.Bits(); i < len(b)*8; i++ {
			if rng.IntN(2) == 1 {
				b[i/8] |= 0x80 >> (i % 8)
			}
		}
		a, _ := netip.AddrFromSlice(b)
		out = append(out, a)
	}
	return out
}

// Random4: uniform IPv4, mostly misses.
func Random4() []netip.Addr {
	rng := rand.New(rand.NewPCG(5, 6))
	out := make([]netip.Addr, N)
	for i := range out {
		v := rng.Uint32()
		out[i] = netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	return out
}

// Mix: Hits/Random4 50/50, shuffled.
func Mix(db *snapshot.DB) []netip.Addr {
	hits, random := Hits(db, false), Random4()
	out := make([]netip.Addr, N)
	for i := range out {
		src := random
		if i%2 == 0 {
			src = hits
		}
		out[i] = src[i]
	}
	rand.New(rand.NewPCG(7, 8)).Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// Same: one addr N times, like v0.0.19's own benchmark.
func Same() []netip.Addr {
	addr := netip.MustParseAddr("52.94.76.1")
	out := make([]netip.Addr, N)
	for i := range out {
		out[i] = addr
	}
	return out
}

type Set struct {
	Name  string
	Addrs []netip.Addr
}

func All(db *snapshot.DB) []Set {
	return []Set{
		{"same", Same()},
		{"v4hit", Hits(db, false)},
		{"v4random", Random4()},
		{"v4mix", Mix(db)},
		{"v6hit", Hits(db, true)},
	}
}

func HitRate(addrs []netip.Addr, found func(netip.Addr) bool) float64 {
	n := 0
	for _, a := range addrs {
		if found(a) {
			n++
		}
	}
	return float64(n) / float64(len(addrs))
}
