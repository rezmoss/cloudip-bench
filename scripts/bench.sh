#!/usr/bin/env bash
# All tests + benchmarks, raw output to results/.
#
#   scripts/bench.sh            # COUNT=10 runs per benchmark
#   COUNT=3 scripts/bench.sh    # quicker, noisier
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
out=$root/results
count=${COUNT:-10}
old=v0.0.19 # last release on cidranger
new=v0.0.20 # flat index
mkdir -p "$out"
cd "$root"

# benchstat pinned as a go.mod tool dep
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
go build -o "$bin/benchstat" golang.org/x/perf/cmd/benchstat
benchstat=$bin/benchstat

{
  go version
  sysctl -n machdep.cpu.brand_string 2>/dev/null || grep -m1 'model name' /proc/cpuinfo
  echo "count=$count"
} | tee "$out/env.txt"

echo "== tests"
go test -count=1 -race -v ./... 2>&1 | grep -E '^(ok|FAIL|---|PASS)|lpm_test.go' | grep -v -- '--- PASS: TestSyntheticExhaustive/' | tee "$out/tests.txt"

(cd data && go test -count=1 -v ./workload) | grep distinct | tee "$out/workload-distinct.txt"

# same release/ bench file, scratch copy pinned per go-cloudip version
for v in $old $new; do
  echo "== go-cloudip $v"
  tmp=$(mktemp -d)
  cp "$root/release/go.mod" "$root/release/release_test.go" "$tmp/"
  (
    cd "$tmp"
    go mod edit -replace "github.com/rezmoss/cloudip-bench/data=$root/data"
    go get "github.com/rezmoss/go-cloudip@$v" >/dev/null 2>&1
    go mod tidy >/dev/null 2>&1
    go list -m github.com/rezmoss/go-cloudip
    DUMP="$out/answers-$v.tsv" go test -count=1 -run Dump . >/dev/null
    go test -run '^$' -bench . -benchmem -count "$count" . >"$out/release-$v.txt"
    # cpu and mem profiles in separate runs; memprofilerate=1 would swamp the cpu profile
    go test -run '^$' -bench 'LookupAddr/v4hit' -benchtime 3s -cpuprofile cpu.out . >/dev/null
    cp cpu.out "$out/cpu-$v.out"
    go tool pprof -top -nodecount 15 cpu.out >"$out/pprof-cpu-lookup-$v.txt" 2>/dev/null
    go test -run '^$' -bench 'LookupAddr/v4hit' -benchtime 1000000x -memprofile mem.out -memprofilerate 1 . >/dev/null
    go tool pprof -sample_index=alloc_objects -focus 'Detector\).LookupAddr$' -top -nodecount 15 mem.out >"$out/pprof-allocs-lookup-$v.txt" 2>/dev/null
    go test -run '^$' -bench 'NewDetector' -benchtime 5x -memprofile start.out -memprofilerate 1 . >/dev/null
    cp start.out "$out/startup-$v.out"
    go tool pprof -sample_index=alloc_objects -top -nodecount 15 start.out >"$out/pprof-allocs-startup-$v.txt" 2>/dev/null
    go tool pprof -sample_index=alloc_objects -peek 'cidranger/net.Network.String$' start.out >"$out/pprof-peek-network-string-$v.txt" 2>/dev/null || true
  )
  rm -rf "$tmp"
done
(cd "$out" && $benchstat release-$old.txt release-$new.txt) | tee "$out/release.txt"
go run ./cmd/answerdiff "$out/answers-$old.tsv" "$out/answers-$new.tsv" | tee "$out/answers.txt"

echo "== same contract, every index"
go test -run '^$' -bench . -benchmem -count "$count" ./lpm >"$out/lpm.txt"
(cd "$out" && $benchstat -col /impl lpm.txt) >"$out/lpm-by-impl.txt"
go run ./cmd/benchtable "$out/lpm.txt" | tee "$out/lpm-table.txt"
grep -E '^(goos|goarch|pkg|cpu):|^BenchmarkHarness' "$out/lpm.txt" >"$out/harness-raw.txt"
(cd "$out" && $benchstat harness-raw.txt) | tee "$out/harness.txt"
go test -count=1 -run Memory -v ./lpm | grep 'MB more' | tee "$out/memory.txt"
