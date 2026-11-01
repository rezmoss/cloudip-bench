// Command benchtable: median ns/op from `go test -bench` output as a grid,
// rows = case, cols = /impl=NAME, lowest median marked.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
)

type grid struct {
	rows, cols []string
	samples    map[[2]string][]float64
}

func main() {
	flag.Parse()
	in := os.Stdin
	if flag.NArg() == 1 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		in = f
	}
	g, err := parse(in)
	if err != nil {
		log.Fatal(err)
	}
	g.print()
}

func parse(r io.Reader) (*grid, error) {
	g := &grid{samples: map[[2]string][]float64{}}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") || fields[3] != "ns/op" {
			continue
		}
		name := strings.TrimPrefix(fields[0], "Benchmark")
		if i := strings.LastIndex(name, "-"); i > 0 {
			name = name[:i] // strip -GOMAXPROCS
		}
		row, col, ok := strings.Cut(name, "/impl=")
		if !ok {
			continue
		}
		ns, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		if !slices.Contains(g.rows, row) {
			g.rows = append(g.rows, row)
		}
		if !slices.Contains(g.cols, col) {
			g.cols = append(g.cols, col)
		}
		key := [2]string{row, col}
		g.samples[key] = append(g.samples[key], ns)
	}
	return g, sc.Err()
}

func (g *grid) print() {
	fmt.Printf("%-18s", "median ns/op")
	for _, c := range g.cols {
		fmt.Printf("%16s", c)
	}
	fmt.Println()
	for _, r := range g.rows {
		best := g.lowest(r)
		fmt.Printf("%-18s", r)
		for _, c := range g.cols {
			v := median(g.samples[[2]string{r, c}])
			switch {
			case v == 0:
				fmt.Printf("%16s", "")
			case v == best:
				fmt.Printf("%15s*", format(v))
			default:
				fmt.Printf("%16s", format(v))
			}
		}
		fmt.Println()
	}
	fmt.Println("\n* lowest median in the row, not a significance test (see lpm-by-impl.txt)")
	fmt.Println("Startup: cloudip decodes its own data; the others start from already decoded rows")
}

func (g *grid) lowest(row string) float64 {
	best := -1.0
	for _, c := range g.cols {
		if v := median(g.samples[[2]string{row, c}]); v > 0 && (best < 0 || v < best) {
			best = v
		}
	}
	return best
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	if n := len(s); n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[len(s)/2]
}

func format(ns float64) string {
	switch {
	case ns >= 1e6:
		return fmt.Sprintf("%.1f ms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.2f µs", ns/1e3)
	default:
		return fmt.Sprintf("%.1f ns", ns)
	}
}
