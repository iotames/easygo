package main

import (
	"flag"
	"fmt"

	"github.com/iotames/detl/conf"
	"github.com/iotames/easyconf"
)

var ActiveDsn, Version string
var cf *conf.Conf

func main() {
	err := dbTest()
	if err != nil {
		panic(fmt.Errorf("dbTest:%s", err))
	}
}

func init() {
	var ConfDir, ScriptDir, DbDriver string
	env := easyconf.NewConf()
	env.StringVar(&ConfDir, "CONF_DIR", "conf", "用户配置目录")
	env.StringVar(&ScriptDir, "SCRIPT_DIR", "script", "低代码脚本目录")
	env.StringVar(&DbDriver, "DB_DRIVER", "postgres", "默认的数据库驱动")
	env.StringVar(&ActiveDsn, "ACTIVE_DSN", "user=postgres password=postgres dbname=postgres host=127.0.0.1 port=5432 sslmode=disable search_path=public", "默认的DSN数据源")
	env.Parse()
	cf = conf.GetConf(ConfDir)
	cf.SetScriptDir(ScriptDir)
	flag.StringVar(&Version, "version", "unstable", "显示版本信息")
	flag.Parse()
	cf = conf.GetConf("")
	fmt.Println("GetScriptDir:", cf.GetScriptDir())
	cf.InitDSN(DbDriver, ActiveDsn)
}
