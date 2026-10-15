// geotags печатает категории в geosite/geoip .dat-файлах.
// go run ./tools/geotags geosite.dat geoip.dat
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/xtls/xray-core/common/geodata"
	"google.golang.org/protobuf/proto"
)

func main() {
	for _, f := range os.Args[1:] {
		b, err := os.ReadFile(f)
		if err != nil {
			panic(err)
		}
		var codes []string
		if strings.Contains(strings.ToLower(f), "geosite") {
			var l geodata.GeoSiteList
			if err := proto.Unmarshal(b, &l); err != nil {
				panic(err)
			}
			for _, e := range l.Entry {
				codes = append(codes, fmt.Sprintf("%s(%d)", strings.ToLower(e.Code), len(e.Domain)))
			}
		} else {
			var l geodata.GeoIPList
			if err := proto.Unmarshal(b, &l); err != nil {
				panic(err)
			}
			for _, e := range l.Entry {
				codes = append(codes, fmt.Sprintf("%s(%d)", strings.ToLower(e.Code), len(e.Cidr)))
			}
		}
		fmt.Printf("%s: %s\n", f, strings.Join(codes, " "))
	}
}
