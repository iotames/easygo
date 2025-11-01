package main

import (
	"flag"
)

var version bool
var opt, key string

func parseCmdArgs() {
	flag.BoolVar(&version, "version", false, "显示版本信息")
	flag.StringVar(&opt, "opt", "", "操作类型: lock, unlock")
	flag.StringVar(&key, "key", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "密钥")
	flag.Parse()
}
