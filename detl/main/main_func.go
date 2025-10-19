package main

import (
	"database/sql"
	"log"

	"github.com/iotames/detl"
	"github.com/iotames/easydb"
	pkgdsn "github.com/iotames/easydb/dsn"
	_ "github.com/lib/pq"
)

func getUserSQL() string {
	sqltxt, err := detl.GetSqlText(cf, "getusers.sql")
	if err != nil {
		log.Fatal(err)
	}
	return sqltxt
}

func dbTest() error {
	var sqldb *sql.DB
	var err error
	dsnconf := pkgdsn.GetDsnConf(nil)
	dgp := pkgdsn.DsnGroup{}
	dsnconf.GetDsnGroup(&dgp)
	for _, ds := range dgp.DsnList {
		sqldb, err = sql.Open(ds.DriverName, ds.Dsn)
		if err != nil {
			return err
		}

		d := easydb.NewEasyDbBySqlDB(sqldb)
		// 这个很少用。是关闭整个连接池。
		defer d.CloseDb()

		var datalist []map[string]interface{}

		err = d.GetMany(getUserSQL(), &datalist)
		if err != nil {
			return err
		}
		for i := range datalist {
			log.Printf("---GetMany--dsncode(%s)--row(%d)---result(%+v)----\n", ds.Code, i, d.DecodeInterface(datalist[i]))
		}
	}
	return err
}
