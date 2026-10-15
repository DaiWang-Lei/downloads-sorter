package sorter

import (
	"syscall"
	"time"
)

func statCtime(st *syscall.Stat_t) time.Time { return time.Unix(st.Ctimespec.Unix()) }
