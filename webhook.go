package main

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"log"

	"github.com/iotames/easyconf"
	"github.com/iotames/easydb"
	"github.com/iotames/easyserver/httpsvr"
	"github.com/iotames/easyserver/response"
	_ "github.com/lib/pq"
)

var DbDriverName, DbHost, DbUser, DbPassword, DbName string
var DbPort, WebPort int

var d *easydb.EasyDb

func main() {
	var err error
	var hdrb []byte
	var sqlresult sql.Result
	// 关闭整个d连接池
	defer d.CloseDb()
	s := httpsvr.NewEasyServer(fmt.Sprintf(":%d", WebPort))
	s.AddMiddleware(httpsvr.NewMiddleCORS("*"))
	s.AddHandler("GET", "/cdnauth", func(ctx httpsvr.Context) {
		sql := `INSERT INTO qiniu_cdnauth_requests (request_id,client_ip,x_forwarded_for,user_agent,http_referer,request_url,request_headers,raw_url) VALUES ($1,$2,$3,$4,$5,$6,$7, $8)`
		q := ctx.Request.URL.Query()
		request_headers := ""
		hdrb, err = json.Marshal(ctx.Request.Header)
		if err == nil {
			request_headers = string(hdrb)
		}
		sqlresult, err = d.Exec(sql,
			q.Get("clientrequestid"),
			q.Get("clientip"),
			q.Get("clientxforwardedfor"),
			q.Get("clientua"),
			q.Get("clientreferer"),
			q.Get("requesturl"),
			request_headers,
			ctx.Request.URL.String(),
		)
		if err != nil {
			log.Println("sqlresult error:", err)
		} else {
			var n int64
			n, err = sqlresult.RowsAffected()
			log.Println("SUCCESS sqlresult:", n, err)
		}
		ctx.Writer.Write(response.NewApiDataOk("success").Bytes())
	})
	s.ListenAndServe()
}

func init() {
	var err error
	cf := easyconf.NewConf()
	cf.StringVar(&DbDriverName, "DB_DRIVER_NAME", "postgres", "数据库驱动名称")
	cf.StringVar(&DbHost, "DB_HOST", "172.16.160.12", "数据库主机地址")
	cf.StringVar(&DbUser, "DB_USER", "postgres", "数据库用户名")
	cf.StringVar(&DbPassword, "DB_PASSWORD", "postgres", "数据库密码")
	cf.StringVar(&DbName, "DB_NAME", "qiniudb", "数据库名称")
	cf.IntVar(&DbPort, "DB_PORT", 5432, "数据库端口")
	cf.IntVar(&WebPort, "WEB_PORT", 1212, "web服务端口")
	cf.Parse()

	sqlCreateTable := `CREATE TABLE IF NOT EXISTS qiniu_cdnauth_requests (
        id SERIAL PRIMARY KEY,
		request_id int8,
		client_ip VARCHAR(45),
		x_forwarded_for VARCHAR(255),
		user_agent VARCHAR(500),
		http_referer VARCHAR(255),
		request_url varchar(1000) NOT NULL,
		request_headers json NOT NULL,
		raw_url varchar(1000) NOT NULL,
		deleted_at timestamp NULL,
		created_at timestamp DEFAULT CURRENT_TIMESTAMP,
		updated_at timestamp DEFAULT CURRENT_TIMESTAMP
    );
CREATE UNIQUE INDEX IF NOT EXISTS "UQE_client_ip" ON qiniu_cdnauth_requests USING btree (client_ip);
CREATE UNIQUE INDEX IF NOT EXISTS "UQE_http_referer" ON qiniu_cdnauth_requests USING btree (http_referer);
	`
	d = easydb.NewEasyDb(DbDriverName, DbHost, DbUser, DbPassword, DbName, DbPort)
	// 测试连接d
	if err = d.Ping(); err != nil {
		log.Fatal(err)
		panic(err)
	}
	_, err = d.Exec(sqlCreateTable)
	if err != nil {
		panic(err)
	}
}
