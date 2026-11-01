// Package lpm: bart, cidranger and a naive oracle behind go-cloudip v0.0.20's
// Lookup contract so benchmarks do equal work: longest prefix wins, same row
// picked per prefix, empty region filled from the nearest same-provider
// parent, every lookup returns a filled cloudip.LookupResult.
package lpm

import (
	"cmp"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/gaissmai/bart"
	"github.com/rezmoss/cloudip-bench/data/snapshot"
	cloudip "github.com/rezmoss/go-cloudip"
	"github.com/yl2chen/cidranger"
)

type Looker interface {
	Lookup(netip.Addr) cloudip.LookupResult
}

type Entry struct {
	Prefix    netip.Prefix
	Result    cloudip.LookupResult
	Inherited bool // Result.Region came from a parent prefix
}

// rank = go-cloudip rowRank, lower wins: has region, then not umbrella tag.
func rank(region, service string) int {
	r := 0
	if region == "" {
		r += 2
	}
	if service == "AMAZON" || service == "AZURECLOUD" || strings.HasPrefix(service, "AZURECLOUD.") {
		r++
	}
	return r
}

// Entries: one row per prefix, picked in go-cloudip's order
// (provider idx, rank, service, region), regions inherited.
func Entries(db *snapshot.DB) []Entry { return entries(db, true) }

func entries(db *snapshot.DB, inherit bool) []Entry {
	best := map[netip.Prefix]snapshot.Range{}
	for _, r := range db.Ranges {
		p, err := netip.ParsePrefix(r.CIDR)
		if err != nil || r.Provider < 0 || r.Provider >= len(db.Providers) {
			continue
		}
		p = p.Masked()
		if cur, ok := best[p]; !ok || less(r, cur) {
			best[p] = r
		}
	}
	out := make([]Entry, 0, len(best))
	for p, r := range best {
		out = append(out, Entry{Prefix: p, Result: cloudip.LookupResult{
			Found:    true,
			Provider: cloudip.Provider(db.Providers[r.Provider]),
			Region:   r.Region,
			Service:  r.Service,
			CIDR:     p.String(),
		}})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		return cmp.Or(a.Prefix.Addr().Compare(b.Prefix.Addr()), cmp.Compare(a.Prefix.Bits(), b.Prefix.Bits()))
	})
	if inherit {
		inheritRegions(out)
	}
	return out
}

// inheritRegions: empty region <- nearest same-provider parent's answer
// (itself maybe inherited), like go-cloudip. es sorted, parents first.
func inheritRegions(es []Entry) {
	parent := make([]int, len(es))
	var stack []int
	for i := range es {
		for len(stack) > 0 && !es[stack[len(stack)-1]].Prefix.Contains(es[i].Prefix.Addr()) {
			stack = stack[:len(stack)-1]
		}
		parent[i] = -1
		if len(stack) > 0 {
			parent[i] = stack[len(stack)-1]
		}
		stack = append(stack, i)
		if es[i].Result.Region != "" {
			continue
		}
		for p := parent[i]; p >= 0; p = parent[p] {
			if es[p].Result.Provider == es[i].Result.Provider && es[p].Result.Region != "" {
				es[i].Result.Region = es[p].Result.Region
				es[i].Inherited = true
				break
			}
		}
	}
}

func less(a, b snapshot.Range) bool {
	return cmp.Or(
		cmp.Compare(a.Provider, b.Provider),
		cmp.Compare(rank(a.Region, a.Service), rank(b.Region, b.Service)),
		strings.Compare(a.Service, b.Service),
		strings.Compare(a.Region, b.Region),
	) < 0
}

// norm: go-cloudip query rules, mapped v6 -> v4, zone ignored.
func norm(a netip.Addr) netip.Addr {
	return a.Unmap().WithZone("")
}

// Bart: bart.Table, int32 payload = index into results.
type Bart struct {
	t       bart.Table[int32]
	results []cloudip.LookupResult
}

func NewBart(es []Entry) *Bart {
	b := &Bart{results: results(es)}
	for i, e := range es {
		b.t.Insert(e.Prefix, int32(i))
	}
	return b
}

func (b *Bart) Lookup(a netip.Addr) cloudip.LookupResult {
	i, ok := b.t.Lookup(norm(a))
	if !ok {
		return cloudip.LookupResult{}
	}
	return b.results[i]
}

// Contains: any-cloud check, no result built.
func (b *Bart) Contains(a netip.Addr) bool { return b.t.Contains(norm(a)) }

// BartFast: bart.Fast, more memory for faster lookups.
type BartFast struct {
	t       bart.Fast[int32]
	results []cloudip.LookupResult
}

func NewBartFast(es []Entry) *BartFast {
	b := &BartFast{results: results(es)}
	for i, e := range es {
		b.t.Insert(e.Prefix, int32(i))
	}
	return b
}

func (b *BartFast) Lookup(a netip.Addr) cloudip.LookupResult {
	i, ok := b.t.Lookup(norm(a))
	if !ok {
		return cloudip.LookupResult{}
	}
	return b.results[i]
}

func (b *BartFast) Contains(a netip.Addr) bool { return b.t.Contains(norm(a)) }

func results(es []Entry) []cloudip.LookupResult {
	out := make([]cloudip.LookupResult, len(es))
	for i, e := range es {
		out[i] = e.Result
	}
	return out
}

type rangerEntry struct {
	n    net.IPNet
	bits int
	i    int32
}

func (e *rangerEntry) Network() net.IPNet { return e.n }

// Ranger: cidranger as go-cloudip v0.0.19 used it, all containing nets, keep longest.
type Ranger struct {
	r       cidranger.Ranger
	results []cloudip.LookupResult
}

func NewRanger(es []Entry) *Ranger {
	r := &Ranger{r: cidranger.NewPCTrieRanger(), results: results(es)}
	for i, e := range es {
		_, n, _ := net.ParseCIDR(e.Prefix.String())
		_ = r.r.Insert(&rangerEntry{*n, e.Prefix.Bits(), int32(i)})
	}
	return r
}

func (r *Ranger) Lookup(a netip.Addr) cloudip.LookupResult {
	a = norm(a)
	if !a.IsValid() {
		return cloudip.LookupResult{}
	}
	es, err := r.r.ContainingNetworks(net.IP(a.AsSlice()))
	if err != nil {
		return cloudip.LookupResult{}
	}
	best, bits := int32(-1), -1
	for _, e := range es {
		if x := e.(*rangerEntry); x.bits > bits {
			best, bits = x.i, x.bits
		}
	}
	if best < 0 {
		return cloudip.LookupResult{}
	}
	return r.results[best]
}

// Oracle: slow but obviously right, probe a map for every len, longest first.
type Oracle struct {
	m       map[netip.Prefix]int32
	results []cloudip.LookupResult
}

func NewOracle(es []Entry) *Oracle {
	oracle := &Oracle{m: make(map[netip.Prefix]int32, len(es)), results: make([]cloudip.LookupResult, len(es))}
	for i, e := range es {
		oracle.m[e.Prefix] = int32(i)
		oracle.results[i] = e.Result
	}
	return oracle
}

func (o *Oracle) Lookup(a netip.Addr) cloudip.LookupResult {
	if i := o.find(a); i >= 0 {
		return o.results[i]
	}
	return cloudip.LookupResult{}
}

// find returns the index of the longest matching prefix, or -1.
func (o *Oracle) find(a netip.Addr) int32 {
	a = norm(a)
	if !a.IsValid() {
		return -1
	}
	for bits := a.BitLen(); bits >= 0; bits-- {
		p, _ := a.Prefix(bits)
		if i, ok := o.m[p]; ok {
			return i
		}
	}
	return -1
}

type CloudIP struct{ D *cloudip.Detector }

func (c CloudIP) Lookup(a netip.Addr) cloudip.LookupResult { return c.D.LookupAddr(a) }
