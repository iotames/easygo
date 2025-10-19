package hotswap

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ScriptDir struct {
	embedFS embed.FS
	dirList []string
}

var onesd *ScriptDir
var once sync.Once

// GetScriptDir 获取脚本目录单例
func GetScriptDir(sd *ScriptDir) *ScriptDir {
	once.Do(func() {
		onesd = sd
	})
	if onesd == nil {
		panic("ScriptDir is nil")
	}
	return onesd
}

// NewScriptDir 初始化程序运行时所需的外部脚本文件目录。
// 如果在给定的所有目录中找不到所需文件，则从embedFs中获取。
// 如果在first_dir找到所需文件，则优先获取。否则继续从more_dirs文件列表中依次获取。
func NewScriptDir(embedFs embed.FS, first_dir string, more_dirs ...string) *ScriptDir {
	return &ScriptDir{embedFS: embedFs, dirList: append([]string{first_dir}, more_dirs...)}
}

func (s ScriptDir) OkDir(d string) error {
	info, err := os.Stat(d)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", d)
		}
		return err
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("dir(%s) does not exist", d)
	}
	return err
}

func (s ScriptDir) OkNormalFile(d string) error {
	info, err := os.Stat(d)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is not a directory", d)
		}
		return err
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("file(%s) does not exist", d)
	}
	return err
}

// GetScriptText 获取脚本文件的纯文本内容
// 优先从dirList目录列表中查找文件。如找不到，最后从内嵌文件中读取。
func (s ScriptDir) GetScriptText(fpath string) (stxt string, err error) {
	var b []byte
	var filepaths []string
	for _, d := range s.dirList {
		filepaths = append(filepaths, filepath.Join(d, fpath))
	}
	realfpath := s.GetFirstExistFile(filepaths...)
	if realfpath != "" {
		// 找到目标文件
		b, err = os.ReadFile(realfpath)
		stxt = string(b)
	}
	if stxt != "" && err == nil {
		// 文件内容不为空，读取没有错误
		return stxt, err
	}
	// 找不到文件，或者虽然找到文件，但文件内容为空或读取错误。则从内嵌的文件中读取sql文件
	b, err = s.embedFS.ReadFile(fpath)
	if err != nil {
		return stxt, err
	}
	stxt = string(b)
	return stxt, err
}

// GetFirstExistFile 从给定的多个文件种，获取第一个存在的文件。
func (s ScriptDir) GetFirstExistFile(filelist ...string) string {
	for _, f := range filelist {
		if s.OkNormalFile(f) == nil {
			return f
		}
	}
	return ""
}

// GetSQL 获取sql文本
// replaceList 字符串列表，依次替换SQL文本中的?占位符
// TODO 需要强调占位符与通配符的区别，比如%和_在LIKE子句中不是占位符，而是通配符，需要和参数化查询中的占位符区分开。
func (s ScriptDir) GetSQL(fpath string, replaceList ...string) (string, error) {
	sqlTxt, err := s.GetScriptText(fpath)
	if err != nil {
		return "", err
	}
	for _, rerplaceStr := range replaceList {
		sqlTxt = strings.Replace(sqlTxt, "?", rerplaceStr, 1)
	}
	return sqlTxt, nil
}
