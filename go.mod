module github.com/rezmoss/cloudip-bench

go 1.26.0

require (
	github.com/gaissmai/bart v0.30.0
	github.com/rezmoss/go-cloudip v0.0.20
	github.com/vmihailenco/msgpack/v5 v5.4.1
	github.com/yl2chen/cidranger v1.0.2
)

require (
	github.com/aclements/go-moremath v0.0.0-20210112150236-f10218a38794 // indirect
	golang.org/x/perf v0.0.0-20261009192801-be2c69fb417e // indirect
)

require (
	github.com/rezmoss/cloudip-bench/data v0.0.0
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
)

replace github.com/rezmoss/cloudip-bench/data => ./data

tool golang.org/x/perf/cmd/benchstat
