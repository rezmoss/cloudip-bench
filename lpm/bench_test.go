package lpm

import (
	"net/netip"
	"runtime"
	"runtime/metrics"
	"testing"

	"github.com/rezmoss/cloudip-bench/data/snapshot"
	"github.com/rezmoss/cloudip-bench/data/workload"
	cloudip "github.com/rezmoss/go-cloudip"
)

const mask = workload.N - 1

var (
	sinkResult cloudip.LookupResult
	sinkBool   bool
	sinkAddr   netip.Addr
)

func detector(b *testing.B) *cloudip.Detector {
	b.Helper()
	d, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = d.Close() })
	return d
}

type impl struct {
	name string
	l    Looker
}

func impls(b *testing.B, es []Entry) []impl {
	return []impl{
		{"cloudip", CloudIP{detector(b)}},
		{"bart", NewBart(es)},
		{"bartfast", NewBartFast(es)},
		{"cidranger", NewRanger(es)},
		{"maplen", NewOracle(es)},
	}
}

func BenchmarkLookup(b *testing.B) {
	d := db(b)
	es := Entries(d)
	im := impls(b, es)
	for _, s := range workload.All(d) {
		hit := workload.HitRate(s.Addrs, func(a netip.Addr) bool { return im[0].l.Lookup(a).Found })
		for _, x := range im {
			b.Run(s.Name+"/impl="+x.name, func(b *testing.B) {
				addrs, l := s.Addrs, x.l
				b.ReportAllocs()
				for i := 0; b.Loop(); i++ {
					sinkResult = l.Lookup(addrs[i&mask])
				}
				b.ReportMetric(hit*100, "hit%")
			})
		}
	}
}

// provider check only, no result built
func BenchmarkIsCloud(b *testing.B) {
	d := db(b)
	det := detector(b)
	es := Entries(d)
	bt, bf := NewBart(es), NewBartFast(es)
	for _, s := range workload.All(d) {
		b.Run(s.Name+"/impl=cloudip", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkBool = det.IsCloudProviderAddr(s.Addrs[i&mask])
			}
		})
		b.Run(s.Name+"/impl=bart", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkBool = bt.Contains(s.Addrs[i&mask])
			}
		})
		b.Run(s.Name+"/impl=bartfast", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkBool = bf.Contains(s.Addrs[i&mask])
			}
		})
	}
}

// from a string, as when parsing log lines
func BenchmarkString(b *testing.B) {
	d := db(b)
	det := detector(b)
	bt := NewBart(Entries(d))
	for _, s := range []workload.Set{{Name: "v4hit", Addrs: workload.Hits(d, false)}, {Name: "v4mix", Addrs: workload.Mix(d)}, {Name: "v6hit", Addrs: workload.Hits(d, true)}} {
		strs := make([]string, len(s.Addrs))
		for i, a := range s.Addrs {
			strs[i] = a.String()
		}
		b.Run(s.Name+"/impl=cloudip", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkResult = det.Lookup(strs[i&mask])
			}
		})
		b.Run(s.Name+"/impl=bart", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				a, err := netip.ParseAddr(strs[i&mask])
				if err == nil {
					sinkResult = bt.Lookup(a)
				}
			}
		})
		// parse cost alone; not an impl, so benchtable skips it
		b.Run(s.Name+"/netip.ParseAddr", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkAddr, _ = netip.ParseAddr(strs[i&mask])
			}
		})
	}
}

// all cores, one shared index
func BenchmarkParallel(b *testing.B) {
	d := db(b)
	es := Entries(d)
	addrs := workload.Mix(d)
	for _, x := range impls(b, es)[:4] {
		b.Run("v4mix/impl="+x.name, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				var r cloudip.LookupResult // per worker, shared sink races
				for i := 0; pb.Next(); i++ {
					r = x.l.Lookup(addrs[i&mask])
				}
				runtime.KeepAlive(r)
			})
		})
	}
}

// go-cloudip includes decoding; bart/cidranger start from decoded rows, so
// decode is measured separately.
func BenchmarkStartup(b *testing.B) {
	d := db(b)
	b.Run("impl=cloudip", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			det, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
			if err != nil {
				b.Fatal(err)
			}
			_ = det.Close()
		}
	})
	// decode alone; not an impl, so benchtable skips it
	b.Run("decode-msgpack", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := snapshot.Load(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("impl=bart", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewBart(Entries(d))
		}
	})
	b.Run("impl=bartfast", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewBartFast(Entries(d))
		}
	})
	b.Run("impl=cidranger", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewRanger(Entries(d))
		}
	})
}

// liveHeap: reachable heap after a forced GC.
func liveHeap() uint64 {
	runtime.GC()
	sample := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

// extra live heap per index: rows already decoded; bart/cidranger add trie +
// result table (sharing row strings), go-cloudip its whole Detector incl. decode.
func TestMemory(t *testing.T) {
	es := Entries(db(t))
	var keep []any
	for _, name := range []string{"bart", "bartfast", "cloudip", "cidranger"} {
		before := liveHeap()
		switch name {
		case "cloudip":
			d, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
			if err != nil {
				t.Fatal(err)
			}
			keep = append(keep, d)
		case "bart":
			keep = append(keep, NewBart(es))
		case "bartfast":
			keep = append(keep, NewBartFast(es))
		case "cidranger":
			keep = append(keep, NewRanger(es))
		}
		t.Logf("%-10s %6.1f MB more live heap after build", name, float64(liveHeap()-before)/1e6)
	}
	runtime.KeepAlive(keep)
}

// same go-cloudip lookup, direct vs through CloudIP + Looker as BenchmarkLookup
// calls it; the gap is that whole call path, not dispatch alone.
func BenchmarkHarness(b *testing.B) {
	d := db(b)
	addrs := workload.Hits(d, false)
	det := detector(b)
	var viaIface Looker = CloudIP{det}
	direct := func(name string, det *cloudip.Detector) {
		b.Run(name, func(b *testing.B) {
			for i := 0; b.Loop(); i++ {
				sinkResult = det.LookupAddr(addrs[i&mask]) // same loop as release/
			}
		})
	}
	iface := func(name string) {
		b.Run(name, func(b *testing.B) {
			for i := 0; b.Loop(); i++ {
				sinkResult = viaIface.Lookup(addrs[i&mask])
			}
		})
	}
	direct("direct", det)
	iface("interface")

	es := Entries(d)
	others := []any{NewBart(es), NewBartFast(es), NewRanger(es), NewOracle(es)}
	direct("direct/others-built", det)
	iface("interface/others-built")
	direct("direct/detector-built-after-others", detector(b))
	runtime.KeepAlive(others)
}
