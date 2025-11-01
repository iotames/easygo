package main

import (
	"github.com/iotames/easygo/lockunlock"
)

func lockopt() error {
	var err error
	skey := []byte(key)
	switch opt {
	case "lock":
		err = lockunlock.LockDirFiles(skey)
	case "unlock":
		err = lockunlock.UnlockDirFiles(skey)
	default:
		// err = fmt.Errorf("opt错误: 未知的操作类型:%s", opt)
		err = showLockUnlockDialog(skey)
	}
	return err
}
