package lpm

import (
	"compress/gzip"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"

	"github.com/rezmoss/cloudip-bench/data/snapshot"
	"github.com/rezmoss/cloudip-bench/data/workload"
	cloudip "github.com/rezmoss/go-cloudip"
	"github.com/vmihailenco/msgpack/v5"
)

var loadSnapshot = sync.OnceValues(snapshot.Load)

func db(tb testing.TB) *snapshot.DB {
	tb.Helper()
	d, err := loadSnapshot()
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// probes: first/last addr of every prefix and both neighbours, where flat tables break.
func probes(es []Entry) []netip.Addr {
	out := make([]netip.Addr, 0, 4*len(es))
	for _, e := range es {
		first, last := e.Prefix.Addr(), lastAddr(e.Prefix)
		out = append(out, first, last)
		if p := first.Prev(); p.IsValid() {
			out = append(out, p)
		}
		if n := last.Next(); n.IsValid() {
			out = append(out, n)
		}
	}
	return out
}

type tally struct {
	checked, inherited int
}

// check: go-cloudip must equal the oracle.
func (tl *tally) check(t *testing.T, a netip.Addr, got cloudip.LookupResult, o *Oracle, es []Entry) {
	t.Helper()
	tl.checked++
	i := o.find(a)
	var want cloudip.LookupResult
	if i >= 0 {
		want = es[i].Result
		if es[i].Inherited {
			tl.inherited++
		}
	}
	if got != want {
		t.Fatalf("%v: go-cloudip %+v, oracle %+v", a, got, want)
	}
}

func TestRealSnapshot(t *testing.T) {
	d := db(t)
	es := Entries(d)
	o := NewOracle(es)
	b := NewBart(es)
	bf := NewBartFast(es)
	r := NewRanger(es)
	det, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		t.Fatal(err)
	}
	defer det.Close()
	if det.Version() != d.Version {
		t.Fatalf("go-cloudip data %s, snapshot %s", det.Version(), d.Version)
	}

	addrs := probes(es)
	for _, s := range workload.All(d) {
		addrs = append(addrs, s.Addrs...)
	}
	var tl tally
	for _, a := range addrs {
		want := o.Lookup(a)
		if got := b.Lookup(a); got != want {
			t.Fatalf("bart %v: %+v, oracle %+v", a, got, want)
		}
		if got := bf.Lookup(a); got != want {
			t.Fatalf("bart.Fast %v: %+v, oracle %+v", a, got, want)
		}
		if got := r.Lookup(a); got != want {
			t.Fatalf("cidranger %v: %+v, oracle %+v", a, got, want)
		}
		tl.check(t, a, det.LookupAddr(a), o, es)
	}
	t.Logf("%d rows, %d distinct prefixes, %d addresses checked against the oracle, %d answers with an inherited region",
		len(d.Ranges), len(es), tl.checked, tl.inherited)
}

func TestQueryEdges(t *testing.T) {
	det, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		t.Fatal(err)
	}
	defer det.Close()
	o := NewOracle(Entries(db(t)))

	v4 := netip.MustParseAddr("52.94.76.1")
	if got, want := det.LookupAddr(netip.AddrFrom16(v4.As16())), det.LookupAddr(v4); got != want || !got.Found {
		t.Fatalf("IPv4-mapped: %+v, IPv4: %+v", got, want)
	}
	if got := det.LookupAddr(netip.Addr{}); got.Found {
		t.Fatalf("zero Addr matched: %+v", got)
	}
	for _, s := range []string{"0.0.0.0", "255.255.255.255", "::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", "2600:1f18::1%eth0"} {
		a := netip.MustParseAddr(s)
		if got, want := det.LookupAddr(a), o.Lookup(a); got.CIDR != want.CIDR {
			t.Fatalf("%s: go-cloudip %q, oracle %q", s, got.CIDR, want.CIDR)
		}
	}
	for _, s := range []string{"", "1.2.3", "1.2.3.4.5", "01.2.3.4", "256.1.1.1", "1.2.3.-4", "not-an-ip"} {
		if got := det.Lookup(s); got.Found {
			t.Fatalf("Lookup(%q) matched %+v", s, got)
		}
	}
}

// synthetic: random table in base, nested/adjacent/dup prefixes, mixed providers, empty regions.
func synthetic(rng *rand.Rand, base netip.Prefix, n int) *snapshot.DB {
	d := &snapshot.DB{Version: "synthetic", Providers: []string{"aws", "azure", "gcp"}}
	maxBits := base.Addr().BitLen()
	for range n {
		bits := base.Bits() + rng.IntN(maxBits-base.Bits()+1)
		b := base.Addr().AsSlice()
		for i := base.Bits(); i < bits; i++ {
			if rng.IntN(2) == 1 {
				b[i/8] |= 0x80 >> (i % 8)
			}
		}
		a, _ := netip.AddrFromSlice(b)
		p := netip.PrefixFrom(a, bits)
		reps := 1 + rng.IntN(3) // dups
		for range reps {
			r := snapshot.Range{CIDR: p.String(), Provider: rng.IntN(3)}
			if rng.IntN(3) > 0 {
				r.Region = []string{"us-east-1", "eu-west-1", "northcentralus"}[rng.IntN(3)]
			}
			r.Service = []string{"EC2", "S3", "AMAZON", "AZURECLOUD", "STORAGE", "SQL", ""}[rng.IntN(7)]
			d.Ranges = append(d.Ranges, r)
		}
	}
	return d
}

// serve: go-cloudip downloads d from a local server.
func serve(t *testing.T, d *snapshot.DB) *cloudip.Detector {
	t.Helper()
	body, err := msgpack.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zw := gzip.NewWriter(w)
		_, _ = zw.Write(body)
		_ = zw.Close()
	}))
	t.Cleanup(srv.Close)
	det, err := cloudip.NewDetector(cloudip.WithDataURL(srv.URL), cloudip.WithNoCache())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = det.Close() })
	if det.Version() != "synthetic" {
		t.Fatalf("go-cloudip loaded %q, not the synthetic table", det.Version())
	}
	return det
}

// every addr of a /16 or /112 vs oracle, many random tables, some with /0
// and the family's last addr.
func TestSyntheticExhaustive(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	type space struct {
		base  netip.Prefix
		extra []string
	}
	spaces := []space{
		{netip.MustParsePrefix("10.20.0.0/16"), nil},
		{netip.MustParsePrefix("255.255.0.0/16"), []string{"0.0.0.0/0", "255.255.255.255/32"}},
		{netip.MustParsePrefix("2001:db8::/112"), nil},
		{netip.MustParsePrefix("ffff:ffff:ffff:ffff:ffff:ffff:ffff:0/112"), []string{"::/0", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128"}},
	}
	tables, total := 0, tally{}
	for _, sp := range spaces {
		for i := range 40 {
			d := synthetic(rng, sp.base, 1+rng.IntN(60))
			if i%2 == 0 {
				for _, c := range sp.extra {
					d.Ranges = append(d.Ranges, snapshot.Range{CIDR: c, Provider: 1, Service: "AZURECLOUD"})
				}
			}
			t.Run(fmt.Sprintf("%s/%d", sp.base, i), func(t *testing.T) {
				det := serve(t, d)
				es := Entries(d)
				o := NewOracle(es)
				var tl tally
				first := sp.base.Addr()
				a := first
				for {
					tl.check(t, a, det.LookupAddr(a), o, es)
					if !sp.base.Contains(a.Next()) {
						break
					}
					a = a.Next()
				}
				for _, s := range []string{"0.0.0.0", "255.255.255.255", "::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
					x := netip.MustParseAddr(s)
					tl.check(t, x, det.LookupAddr(x), o, es)
				}
				total.checked += tl.checked
				total.inherited += tl.inherited
			})
			tables++
		}
	}
	t.Logf("%d synthetic tables, %d addresses checked, %d inherited regions", tables, total.checked, total.inherited)
}

// shuffled rows must give the same answers (v0.0.19 didn't).
func TestOrderIndependent(t *testing.T) {
	d := db(t)
	shuffled := *d
	shuffled.Version = "synthetic"
	shuffled.Ranges = append([]snapshot.Range(nil), d.Ranges...)
	rand.New(rand.NewPCG(13, 14)).Shuffle(len(shuffled.Ranges), func(i, j int) {
		shuffled.Ranges[i], shuffled.Ranges[j] = shuffled.Ranges[j], shuffled.Ranges[i]
	})
	a, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b := serve(t, &shuffled)
	addrs := probes(Entries(d))
	for _, x := range addrs {
		if ra, rb := a.LookupAddr(x), b.LookupAddr(x); ra != rb {
			t.Fatalf("%v: file order %+v, shuffled %+v", x, ra, rb)
		}
	}
	t.Logf("%d probes give the same answer after shuffling %d rows", len(addrs), len(d.Ranges))
}

// region rule: nearest parent wins, other providers skipped, inherited parent
// region counts, same-prefix provider tie -> provider order.
func TestInheritance(t *testing.T) {
	d := &snapshot.DB{Version: "synthetic", Providers: []string{"aws", "azure"}, Ranges: []snapshot.Range{
		{CIDR: "10.0.0.0/8", Provider: 0, Region: "far", Service: "A"},
		{CIDR: "10.1.0.0/16", Provider: 0, Region: "near", Service: "B"},
		{CIDR: "10.1.2.0/24", Provider: 0, Service: "C"},
		{CIDR: "11.0.0.0/8", Provider: 0, Region: "aws-region", Service: "A"},
		{CIDR: "11.1.0.0/16", Provider: 1, Region: "azure-region", Service: "X"},
		{CIDR: "11.1.2.0/24", Provider: 0, Service: "C"},
		{CIDR: "12.0.0.0/8", Provider: 0, Region: "top", Service: "A"},
		{CIDR: "12.1.0.0/16", Provider: 0, Service: "B"},
		{CIDR: "12.1.2.0/24", Provider: 0, Service: "C"},
		{CIDR: "13.0.0.0/24", Provider: 1, Region: "azure-region", Service: "X"},
		{CIDR: "13.0.0.0/24", Provider: 0, Region: "aws-region", Service: "Y"},
		{CIDR: "14.0.0.0/24", Provider: 0, Service: "C"},
	}}
	det := serve(t, d)
	es := Entries(d)
	o := NewOracle(es)
	for _, c := range []struct{ ip, provider, region, service string }{
		{"10.1.2.3", "aws", "near", "C"},
		{"11.1.2.3", "aws", "aws-region", "C"},
		{"12.1.0.1", "aws", "top", "B"},
		{"12.1.2.3", "aws", "top", "C"},
		{"13.0.0.1", "aws", "aws-region", "Y"},
		{"14.0.0.1", "aws", "", "C"},
	} {
		a := netip.MustParseAddr(c.ip)
		got := det.LookupAddr(a)
		if string(got.Provider) != c.provider || got.Region != c.region || got.Service != c.service {
			t.Errorf("%s: go-cloudip %+v, want %s/%s/%s", c.ip, got, c.provider, c.region, c.service)
		}
		if want := o.Lookup(a); want != got {
			t.Errorf("%s: oracle %+v, go-cloudip %+v", c.ip, want, got)
		}
	}
}

// tests must be able to fail: a no-inheritance oracle must disagree somewhere.
func TestOracleWithoutInheritanceFails(t *testing.T) {
	d := db(t)
	det, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		t.Fatal(err)
	}
	defer det.Close()
	es := entries(d, false)
	o := NewOracle(es)
	mismatches := 0
	for _, a := range probes(es) {
		if det.LookupAddr(a) != o.Lookup(a) {
			mismatches++
		}
	}
	if mismatches == 0 {
		t.Fatal("oracle without inheritance matched go-cloudip everywhere; the tests can't tell")
	}
	t.Logf("without inheritance the oracle disagrees on %d probes", mismatches)
}
