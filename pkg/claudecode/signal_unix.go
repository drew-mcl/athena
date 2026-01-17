//go:build unix

package claudecode

import (
	"os"
	"syscall"
)

func interruptSignal() os.Signal {
	return syscall.SIGINT
}
