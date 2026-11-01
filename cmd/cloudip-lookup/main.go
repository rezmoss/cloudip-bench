// Command cloudip-lookup prints go-cloudip's Lookup answer per address,
// or every containing record with -all.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	cloudip "github.com/rezmoss/go-cloudip"
)

func main() {
	all := flag.Bool("all", false, "print every record containing the address (LookupAll)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cloudip-lookup [-all] ip...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	d, err := cloudip.NewDetector(cloudip.WithOffline(), cloudip.WithNoCache())
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	fmt.Printf("go-cloudip data %s, %d records\n\n", d.Version(), d.RangeCount())

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ADDRESS\tPROVIDER\tREGION\tSERVICE\tCIDR")
	for _, ip := range flag.Args() {
		rows := []cloudip.LookupResult{d.Lookup(ip)}
		if *all {
			rows = d.LookupAll(ip)
		}
		if len(rows) == 0 || !rows[0].Found {
			fmt.Fprintf(w, "%s\t-\t-\t-\t-\n", ip)
			continue
		}
		for i, r := range rows {
			addr := ip
			if i > 0 {
				addr = ""
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", addr, r.Provider, dash(r.Region), dash(r.Service), r.CIDR)
		}
	}
	if err := w.Flush(); err != nil {
		log.Fatal(err)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
