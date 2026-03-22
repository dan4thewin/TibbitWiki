package wiki

import (
	"os"
	"syscall"
	"time"
)

func atime(fi os.FileInfo) time.Time {
	stat := fi.Sys().(*syscall.Stat_t)
	return time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec)
}
