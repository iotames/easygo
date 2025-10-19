package conf

import (
	"fmt"
	"path/filepath"
	"sync"

	pkgdsn "github.com/iotames/easydb/dsn"
	"github.com/iotames/miniutils"
)

var cf *Conf
var once sync.Once

func GetConf(confdir string) *Conf {
	once.Do(func() {
		cf = newConf(confdir)
	})
	return cf
}

func newConf(confdir string) *Conf {
	if err := miniutils.Mkdir(confdir); err != nil {
		panic(confdir)
	}
	return &Conf{
		dirPath: confdir,
		envMap:  make(map[string]string, 5),
	}
}

type Conf struct {
	dirPath string
	envMap  map[string]string
}

func (c *Conf) SetScriptDir(d string) error {
	var err error
	if err = miniutils.Mkdir(d); err != nil {
		return err
	}
	c.envMap["SCRIPT_DIR"] = d
	return err
}

func (c Conf) GetScriptDir() string {
	return c.envMap["SCRIPT_DIR"]
}

func (c Conf) GetScriptFilePath(fname string) string {
	return filepath.Join(c.GetScriptDir(), fname)
}

func (c Conf) InitDSN(driverName, dsn string) (dsnconf *pkgdsn.DsnConf, err error, isInit bool) {
	filename := "dsn.json"
	dgp := &pkgdsn.DsnGroup{}
	fpath := filepath.Join(c.dirPath, filename)
	dsnconf = pkgdsn.NewDsnConf(fpath)
	pkgdsn.GetDsnConf(dsnconf)
	if !miniutils.IsPathExists(fpath) {
		fmt.Println("create conf file:", fpath)
		err = dgp.AppendDsn(driverName, dsn)
		if err != nil {
			return dsnconf, err, true
		}
		err = dsnconf.SaveDsnGroup(*dgp)
		return dsnconf, err, true
	}
	return dsnconf, err, false
}

func (c Conf) SetActiveDSN(driverName, dsn string) error {
	dsnconf, err, isInit := c.InitDSN(driverName, dsn)
	if err != nil {
		return err
	}
	if isInit {
		return err
	}
	dgp := &pkgdsn.DsnGroup{}
	err = dsnconf.GetDsnGroup(dgp)
	if err != nil {
		return err
	}
	dsnCode := miniutils.Md5(dsn)
	if dgp.HasActive(dsnCode) {
		return nil
	}
	if !dgp.HasDsn(dsn) {
		dgp.AppendDsn(driverName, dsn)
	}
	err = dgp.Active(dsnCode)
	if err != nil {
		return err
	}
	return dsnconf.SaveDsnGroup(*dgp)
}
