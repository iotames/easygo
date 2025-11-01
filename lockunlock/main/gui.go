package main

import (
	"github.com/iotames/easygo/lockunlock"
	"github.com/sqweek/dialog"
)

// showLockUnlockDialog 显示文件锁对话框并返回用户选择
func showLockUnlockDialog(skey []byte) error {
	// 显示描述信息
	dialog.Message("加密/解密当前目录所有文件，不包括子目录。").Title("lockunlock文件密码锁：").Info()

	// // 1. 选择目录
	// directory, err := chooseDirectory()
	// if err != nil {
	// 	return nil, err
	// }

	// // 2. 输入密钥
	// key, err := getKeyInput()
	// if err != nil {
	// 	return nil, err
	// }

	// 3. 选择操作
	result := dialog.Message("请选择操作：\n\n点击【是(Y)】加密，【否(N)】解密").Title("选择操作").YesNo()
	if result {
		return lockunlock.LockDirFiles(skey)
	}
	return lockunlock.UnlockDirFiles(skey)
}
