# cloudip-bench

Reproducible benchmarks and correctness tests behind the article
[From 1 µs to 45 ns: Rewriting a Go Cloud IP Lookup on net/netip](https://rezmoss.com/blog/go-netip-cloud-ip-lookup/).

It measures [go-cloudip](https://github.com/rezmoss/go-cloudip) before and after
its rewrite from a [cidranger](https://github.com/yl2chen/cidranger) trie to a flat
`net/netip` index, and compares the new index with [bart](https://github.com/gaissmai/bart)
(`bart.Table` and `bart.Fast`) and cidranger under the same result contract.

| | |
|---|---|
| v0.0.19 | last go-cloudip release on cidranger |
| v0.0.20 | flat sorted index with a /16 directory |
| data | cloudip-db snapshot v2026-10-26 (131,624 records, 83,206 distinct prefixes), the same file both releases embed, SHA-256 checked on load |

## Requirements

Go 1.26 or newer. Nothing else: benchstat is pinned as a `tool` dependency in `go.mod`.

## Run it

```bash
scripts/bench.sh            # everything, 10 runs per benchmark (about 25 minutes)
COUNT=3 scripts/bench.sh    # quicker, noisier
go test ./...               # correctness tests only
go run ./cmd/cloudip-lookup -all 1.178.10.7
```

`scripts/bench.sh` writes raw output to `results/`:

| File | What it is |
|---|---|
| `env.txt` | Go version, CPU, run count |
| `tests.txt` | test log (with `-race`) and the counts each test checked |
| `workload-distinct.txt` | distinct addresses in each sample set |
| `release-v0.0.19.txt`, `release-v0.0.20.txt`, `release.txt` | the same public-API benchmarks run against each release, and benchstat's comparison |
| `answers.txt` | how the answers changed between releases (the per-address dumps `answers-*.tsv` are 15 MB each and not committed) |
| `pprof-*.txt` | CPU and allocation profiles of both releases (lookup and startup) |
| `lpm.txt`, `lpm-table.txt`, `lpm-by-impl.txt` | go-cloudip, bart.Table, bart.Fast, cidranger and a map-per-prefix-length oracle under one contract |
| `memory.txt` | extra live heap each index adds in this harness (decoded rows already exist; bart and cidranger share their strings) |
| `harness.txt` | go-cloudip called directly vs through the `CloudIP` adapter and `Looker` interface inside the `lpm` binary |
| `extras-*.txt` | internal measurements from `extras/` (run inside a go-cloudip checkout), raw and summarized |

## What is measured

Every lookup benchmark replays 65,536 seeded, synthetic samples, drawn with replacement (`v4hit` has 56,842 distinct addresses, `v6hit` 62,824; `(cd data && go test -v ./...)` prints them):

| Set | Addresses |
|---|---|
| `same` | `52.94.76.1` every time, like go-cloudip v0.0.19's own benchmark |
| `v4hit` | a random address inside a record picked uniformly from the snapshot, so prefixes listed in several records get picked more often (100% hits) |
| `v4random` | uniform random IPv4 (mostly misses; the hit rate is reported as `hit%`) |
| `v4mix` | `v4hit` and `v4random` 50/50, shuffled |
| `v6hit` | like `v4hit`, for IPv6 records |

In `lpm/`, bart and cidranger are given go-cloudip's result contract: the longest
matching prefix wins, the same record is chosen among records of one prefix
(provider, then region-before-none and specific-over-umbrella, then service and
region name), an empty region is filled from the nearest enclosing prefix of the
same provider, and every lookup returns a filled `cloudip.LookupResult`. The
inheritance is computed independently from the published records, and the tests
require go-cloudip's answers to match exactly.

`BenchmarkLookup` calls every implementation through the same `Looker`
interface. For go-cloudip's `v4hit` lookup that call arrangement (adapter,
dispatch, result passing) measures 60 ns against 44 ns for a direct call
(`BenchmarkHarness`, `results/harness.txt`); the overhead wasn't measured for the
other implementations and needn't be identical. Compare implementations within the
`Lookup` rows, and use the release benchmark for go-cloudip's own numbers.

## Layout

| Path | |
|---|---|
| `data/snapshot` | the pinned snapshot, embedded and checksum-verified (separate module, so the release benchmarks can pin any go-cloudip version) |
| `data/workload` | the address sets |
| `lpm/` | adapters, correctness tests, same-contract benchmarks |
| `release/` | public-API benchmarks run per release by `scripts/bench.sh` |
| `cmd/cloudip-lookup` | print `Lookup` / `LookupAll` answers for addresses |
| `cmd/answerdiff` | compare two answer dumps |
| `cmd/benchtable` | median ns/op grid from benchmark output |
| `extras/` | internal probes to run inside a go-cloudip v0.0.20 checkout |

## Correctness tests

- every address in the workloads plus the first, last and neighbouring address of every prefix (660k addresses) against a map-per-prefix-length oracle, for go-cloudip, bart and cidranger
- 160 random synthetic tables (nested, adjacent and duplicated prefixes, mixed providers, `/0`, last address of each family) served to go-cloudip over `httptest`, with every address of a /16 or /112 checked (10.5M lookups)
- shuffled input rows give identical answers
- hand-written region inheritance cases: nearest parent, other providers skipped, inherited parent regions, same-prefix provider ties
- IPv4-mapped IPv6, zoned, invalid and edge addresses

## Hardware

All numbers in the article come from one Apple M1 Max (darwin/arm64), Go 1.26.8.
Run it on your own machine; the ratios matter more than the absolute numbers.

## License

MIT. The snapshot is from [cloudip-db](https://github.com/rezmoss/cloudip-db) (MIT).
