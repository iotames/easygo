package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v2"
)

type AreaData struct {
	Code    string              `yaml:"code"`
	TelCode string              `yaml:"telCode"`
	Data    map[string]AreaData `yaml:"data"`
}

// 定义你的YAML结构
type MyYmlBody struct {
	Parameters struct {
		Country map[string]AreaData `yaml:"country"`
	} `yaml:"parameters"`
}

func ymltoxlsx() {
	// 读取YAML文件
	yamlFile, err := os.ReadFile("country_zh.yml")
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	// 解析YAML
	var config MyYmlBody
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	// 打印结果
	// fmt.Printf("------bd(%+v)-----\n", config)
	fpath := time.Now().Format("countryara_0102_150405.csv")
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	var data [][]string
	countsheng := 0
	for k, v := range config.Parameters.Country {
		if k != "中国" {
			continue
		}

		for k1, v1 := range v.Data {
			countsheng++
			fmt.Println(k1)

			for k2, v2 := range v1.Data {
				if len(v2.Data) > 0 {
					for k3, v3 := range v2.Data {
						row := []string{k1, v1.Code, k2, v2.Code, k3, v3.Code}
						fmt.Printf("---%+v---\n", row)
						data = append(data, row)
					}
				} else {
					row := []string{k1, v1.Code, k2, v2.Code}
					fmt.Printf("---%+v---\n", row)
					data = append(data, row)
				}
				println(k1, k2)

			}
			println("--------------------------------")
		}
	}
	fmt.Printf("省级单位有%d个\n", countsheng)
	for _, row := range data {
		err := w.Write(row)
		if err != nil {
			panic(err)
		}
	}

}
