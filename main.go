package main

import (
	"flag"
)

var mp map[string]func() = map[string]func(){
	"ymltoxlsx": ymltoxlsx,
	"codegen":   codegen,
}

func main() {
	var name string
	flag.StringVar(&name, "name", "", "arg for func name")
	fc, ok := mp[name]
	if !ok {
		panic("func name:" + name + ", is not exists")
	}
	fc()
}
