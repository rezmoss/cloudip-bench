// Package snapshot: pinned cloudip-db v2026-10-26, the same data go-cloudip
// v0.0.19 and v0.0.20 embed.
package snapshot

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

//go:embed cloudip.msgpack.gz
var raw []byte

//go:embed version.json
var versionJSON []byte

type Range struct {
	CIDR     string `msgpack:"cidr"`
	Provider int    `msgpack:"p"`
	Region   string `msgpack:"r,omitempty"`
	Service  string `msgpack:"s,omitempty"`
}

type DB struct {
	Version   string   `msgpack:"version"`
	BuildTime int64    `msgpack:"build_time"`
	Providers []string `msgpack:"providers"`
	Ranges    []Range  `msgpack:"ranges"`
}

// Load gunzips, checks SHA-256 vs version.json, decodes.
func Load() (*DB, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var v struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(versionJSON, &v); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != v.SHA256 {
		return nil, fmt.Errorf("snapshot checksum %s, want %s", got, v.SHA256)
	}
	var db DB
	if err := msgpack.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &db, nil
}
