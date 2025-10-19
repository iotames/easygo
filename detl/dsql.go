package detl

import (
	"os"

	"github.com/iotames/detl/conf"
)

// GetSqlText 从配置中获取SQL脚本内容。
// filename 不包含脚本文件基础目录。脚本文件名，比如 "getusers.sql"
func GetSqlText(cf *conf.Conf, filename string) (sqltxt string, err error) {
	fpath := cf.GetScriptFilePath(filename)
	content, err := os.ReadFile(fpath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
