# extras

Measurements that need go-cloudip's unexported internals, so they can't run
from this module.

`index_probe_test.go.txt`: copy into a go-cloudip v0.0.20 checkout as
`index_probe_test.go`, then

```bash
go test -run Shape -v .                              # how the /16 directory splits the space, widest ranges
go test -run '^$' -bench Probe -count 10 . > probe.txt
go -C /path/to/cloudip-bench tool benchstat "$PWD/probe.txt"   # pinned benchstat
```

It times the bare IPv4 index probe as shipped (branchless search), with
`slices.BinarySearch` instead, and without the directory; the IPv6 probe as
shipped against a plain branchless bisect; and the strict IPv4 parser against
`netip.ParseAddr`. Results: `../results/extras-probe.txt`.

The IPv6 end-to-end check (`../results/extras-v6-end-to-end.txt`) used two
copies of v0.0.20. In both, `benchDetector` in `bench_test.go` builds the
detector with `NewDetector(WithOffline(), WithNoCache())`, so it loads the
embedded snapshot and not a newer one from `~/.cache/go-cloudip`. In the second
copy the body of `(*index).lookup6` is replaced with

```go
return ix.refs6[search6(ix.starts6, 0, len(ix.starts6), x)]
```

Five rounds, alternating the two copies, each running

```bash
go test -run '^$' -bench 'LookupAddr_IPv6Hit|Lookup_IPv6Hit' -count 2 .
```

give 10 samples per version, compared with the pinned benchstat as above.
