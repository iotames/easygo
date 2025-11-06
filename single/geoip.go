package main

import (
	"flag"
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
	// "github.com/oschwald/geoip2-golang/v2"
)

var Ip string

func main() {
	flag.StringVar(&Ip, "ip", "36.43.239.202", "ip")
	flag.Parse()
	// https://github.com/P3TERX/GeoLite.mmdb/releases
	// https://github.com/P3TERX/GeoLite.mmdb/releases/download/2025.09.22/GeoLite2-City.mmdb
	db, err := maxminddb.Open("GeoLite2-City.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr(Ip)
	if err != nil {
		log.Fatal(err)
	}

	var record struct {
		Country struct {
			ISOCode string            `maxminddb:"iso_code"`
			Names   map[string]string `maxminddb:"names"`
		} `maxminddb:"country"`
		Subdivisions []struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"subdivisions"`
		City struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"city"`
	}

	err = db.Lookup(ip).Decode(&record)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Country: %s (%s)\n", record.Country.Names["zh-CN"], record.Country.ISOCode)
	fmt.Printf("City: %s\n", record.City.Names["zh-CN"])
	fmt.Printf("Subdivision: %s\n", record.Subdivisions[0].Names["zh-CN"])
	fmt.Printf("CountryInfo: %+v\n", record.Country.Names)
	fmt.Printf("CityInfo: %+v\n", record.City.Names)
}
