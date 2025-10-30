package lockunlock

import (
	"fmt"
	"os/exec"
	"runtime"
)

// ShowMessage 显示系统弹框消息
func ShowMessage(title, message string) {
	switch runtime.GOOS {
	case "windows":
		// Windows系统使用PowerShell显示弹框
		script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('%s', '%s')`, message, title)
		exec.Command("powershell", "-Command", script).Run()
	case "darwin":
		// macOS系统使用osascript显示弹框
		script := fmt.Sprintf(`display dialog "%s" with title "%s" buttons {"OK"}`, message, title)
		exec.Command("osascript", "-e", script).Run()
	default:
		// Linux系统使用zenity显示弹框（如果已安装）
		_, err := exec.LookPath("zenity")
		if err != nil {
			// 如果没有安装zenity，回退到终端输出
			fmt.Printf("%s: %s\n", title, message)
			return
		}
		exec.Command("zenity", "--info", "--title", title, "--text", message).Run()
	}
}
