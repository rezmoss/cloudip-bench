// Command answerdiff counts how answers changed between two release.TestDump
// dumps (old, new).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

type answer struct {
	found, cidr, provider, region, service string
}

func read(path string) (map[string]answer, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	m := map[string]answer{}
	var order []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		if len(p) != 6 {
			return nil, nil, fmt.Errorf("%s: bad line %q", path, sc.Text())
		}
		if _, dup := m[p[0]]; dup {
			return nil, nil, fmt.Errorf("%s: %s listed twice", path, p[0])
		}
		m[p[0]] = answer{p[1], p[2], p[3], p[4], p[5]}
		order = append(order, p[0])
	}
	return m, order, sc.Err()
}

func main() {
	examples := flag.Int("examples", 3, "examples to print per kind of change")
	flag.Parse()
	if flag.NArg() != 2 {
		log.Fatal("usage: answerdiff old.tsv new.tsv")
	}
	oldAnswers, order, err := read(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	newAnswers, _, err := read(flag.Arg(1))
	if err != nil {
		log.Fatal(err)
	}
	if len(oldAnswers) != len(newAnswers) {
		log.Fatalf("dumps differ in size: %d vs %d addresses", len(oldAnswers), len(newAnswers))
	}
	for addr := range oldAnswers {
		if _, ok := newAnswers[addr]; !ok {
			log.Fatalf("%s missing from %s", addr, flag.Arg(1))
		}
	}
	kinds := []string{"same", "service", "region", "service+region", "prefix", "found"}
	count := map[string]int{}
	shown := map[string][]string{}
	for _, addr := range order {
		before, after := oldAnswers[addr], newAnswers[addr]
		kind := classify(before, after)
		count[kind]++
		if kind != "same" && len(shown[kind]) < *examples {
			shown[kind] = append(shown[kind], fmt.Sprintf("  %-16s %-18s %q/%q -> %q/%q",
				addr, after.cidr, before.service, before.region, after.service, after.region))
		}
	}
	fmt.Printf("%d addresses compared\n\n", len(order))
	for _, k := range kinds {
		fmt.Printf("%-15s %7d  %5.1f%%\n", k, count[k], 100*float64(count[k])/float64(len(order)))
	}
	for _, k := range kinds[1:] {
		if len(shown[k]) == 0 {
			continue
		}
		fmt.Printf("\n%s changed (service/region, old -> new):\n%s\n", k, strings.Join(shown[k], "\n"))
	}
}

func classify(before, after answer) string {
	switch {
	case before.found != after.found:
		return "found"
	case before.cidr != after.cidr || before.provider != after.provider:
		return "prefix"
	case before.service != after.service && before.region != after.region:
		return "service+region"
	case before.service != after.service:
		return "service"
	case before.region != after.region:
		return "region"
	}
	return "same"
}
