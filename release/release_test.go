// Package release: public-API-only benchmarks so one file runs on any
// go-cloudip version; bench.sh runs it on v0.0.19 and v0.0.20.
package release

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"testing"

	"github.com/rezmoss/cloudip-bench/data/snapshot"
	"github.com/rezmoss/cloudip-bench/data/workload"
	cloudip "github.com/rezmoss/go-cloudip"
)

const mask = workload.N - 1

var (
	sinkResult cloudip.LookupResult
	sinkBool   bool
)

func setup(tb testing.TB) (*snapshot.DB, *cloudip.Detector) {
	tb.Helper()
	db, err := snapshot.Load()
	if err != nil {
		tb.Fatal(err)
	}
	d, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = d.Close() })
	if d.Version() != db.Version {
		tb.Fatalf("go-cloudip embeds %s, snapshot is %s", d.Version(), db.Version)
	}
	return db, d
}

func BenchmarkLookupAddr(b *testing.B) {
	db, d := setup(b)
	for _, s := range workload.All(db) {
		b.Run(s.Name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkResult = d.LookupAddr(s.Addrs[i&mask])
			}
		})
	}
}

func BenchmarkLookupString(b *testing.B) {
	db, d := setup(b)
	for _, s := range []workload.Set{{Name: "v4hit", Addrs: workload.Hits(db, false)}, {Name: "v6hit", Addrs: workload.Hits(db, true)}} {
		strs := make([]string, len(s.Addrs))
		for i, a := range s.Addrs {
			strs[i] = a.String()
		}
		b.Run(s.Name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				sinkResult = d.Lookup(strs[i&mask])
			}
		})
	}
}

func BenchmarkIsCloudProviderAddr(b *testing.B) {
	db, d := setup(b)
	addrs := workload.Mix(db)
	b.Run("v4mix", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			sinkBool = d.IsCloudProviderAddr(addrs[i&mask])
		}
	})
}

func BenchmarkNewDetector(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		d, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
		if err != nil {
			b.Fatal(err)
		}
		_ = d.Close()
	}
}

// TestDump writes answers to $DUMP for cmd/answerdiff.
func TestDump(t *testing.T) {
	out := os.Getenv("DUMP")
	if out == "" {
		t.Skip("set DUMP=file to write answers")
	}
	db, d := setup(t)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	seen := map[netip.Addr]bool{}
	write := func(a netip.Addr) {
		if !a.IsValid() || seen[a] {
			return
		}
		seen[a] = true
		r := d.LookupAddr(a)
		fmt.Fprintf(w, "%s\t%t\t%s\t%s\t%s\t%s\n", a, r.Found, r.CIDR, r.Provider, r.Region, r.Service)
	}
	for _, r := range db.Ranges {
		p, err := netip.ParsePrefix(r.CIDR)
		if err != nil {
			continue
		}
		write(p.Masked().Addr())
	}
	for _, a := range workload.Hits(db, false) {
		write(a)
	}
	for _, a := range workload.Hits(db, true) {
		write(a)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d addresses written to %s", len(seen), out)
}
