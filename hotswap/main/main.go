package main

import (
	"flag"
)

var debug string

func main() {
	if debug == "workerpool" {
		debug_workerpool()
	}

}

func init() {
	flag.StringVar(&debug, "debug", "", "debug")
	flag.Parse()
}
