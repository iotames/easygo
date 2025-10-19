package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/iotames/easydb"
	_ "github.com/lib/pq"
)

const batchSize = 2000
const maxBatchSize = 20097241
const dbUser = "postgres"
const dbPassword = "postgres"
const dbName = "postgres"
const filePath = "D:\\Users\\santic\\Documents\\filelist_large.txt"

var d *easydb.EasyDb

func main() {
	var sqlresult sql.Result
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close() // 确保在函数返回时关闭文件

	// 关闭整个d连接池
	defer d.CloseDb()

	scanner := bufio.NewScanner(file)
	i := 0
	var rows []string
	for scanner.Scan() { // 逐行读取
		i++
		if i > maxBatchSize {
			break
		}
		// 每次对数据库写入2000条数据
		line := strings.TrimSpace(scanner.Text()) // 获取当前行的文本
		fmt.Println("-----处理行数", i, line)         // 处理每一行，例如打印出来
		rows = append(rows, line)
		if i%batchSize == 0 {
			// fmt.Println("----写入数据库", rows)
			sql := `INSERT INTO filelist (raw_line_data,raw_line_data_len) VALUES %s`
			sqlvalues := []string{}
			sqli := 1
			sqlargs := []interface{}{}
			for _, row := range rows {
				// // sqlvalues = append(sqlvalues, fmt.Sprintf("('%s')", row))
				// escapedRow := strings.Replace(row, "'", "''", -1)
				// sqlvalues = append(sqlvalues, fmt.Sprintf("('%s',%d)", escapedRow, len(escapedRow)))
				sqlargs = append(sqlargs, row, len(row))
				sqlvalues = append(sqlvalues, fmt.Sprintf("($%d,$%d)", sqli, sqli+1))
				sqli += 2
			}
			sql = fmt.Sprintf(sql, strings.Join(sqlvalues, ",")+";")
			// fmt.Printf("------sql(%s)---", sql)
			sqlresult, err = d.Exec(sql, sqlargs...)
			if err != nil {
				panic(err)
			}
			fmt.Println("------写入数据库成功------", sqlresult)
			rows = []string{}
		}
	}
	fmt.Println("-------------------------------------------------")
	i = 0
	if len(rows) > 0 {
		sql := `INSERT INTO filelist (raw_line_data,raw_line_data_len) VALUES %s`
		sqlvalues := []string{}

		sqli := 1
		sqlargs := []interface{}{}
		for _, row := range rows {
			i++
			// escapedRow := strings.Replace(row, "'", "''", -1)
			// sqlvalues = append(sqlvalues, fmt.Sprintf("('%s',%d)", escapedRow, len(escapedRow)))
			sqlargs = append(sqlargs, row, len(row))
			sqlvalues = append(sqlvalues, fmt.Sprintf("($%d,$%d)", sqli, sqli+1))
			sqli += 2
			// TODO
		}
		fmt.Println("最后处理行数", i)

		sql = fmt.Sprintf(sql, strings.Join(sqlvalues, ",")+";")
		// fmt.Printf("------sql(%s)---", sql)
		sqlresult, err = d.Exec(sql, sqlargs...)
		if err != nil {
			panic(err)
		}
		fmt.Println("------写入数据库成功------", sqlresult)

	}

	if err := scanner.Err(); err != nil { // 检查是否有错误发生
		fmt.Println("Error reading file:", err)
	}
}

func init() {
	var err error
	sqlCreateTable := `CREATE TABLE IF NOT EXISTS filelist (
        id SERIAL PRIMARY KEY,
        raw_line_data TEXT,
		raw_line_data_len INT,
        key VARCHAR(255),
		file_size BIGINT,
		hash VARCHAR(64),
		put_time INT,
		mime_type VARCHAR(64),
		file_type VARCHAR(64),
		end_user VARCHAR(255)
    )`
	d = easydb.NewEasyDb("postgres", "172.16.160.12", dbUser, dbPassword, dbName, 5432)
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
