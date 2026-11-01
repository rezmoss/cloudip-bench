module github.com/rezmoss/cloudip-bench/release

go 1.26.0

require (
	github.com/rezmoss/cloudip-bench/data v0.0.0
	github.com/rezmoss/go-cloudip v0.0.20
)

require (
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
)

replace github.com/rezmoss/cloudip-bench/data => ../data
