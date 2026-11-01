package media

import (
	"os/exec"
	"syscall"
)

func init() {
	hideConsole = func(c *exec.Cmd) {
		// CREATE_NO_WINDOW: ffmpeg is a console program and would get a
		// black window of its own next to the GUI client.
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	}
}
