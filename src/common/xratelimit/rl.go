package xratelimit

import (
	"time"
	"sync"
)

type RL struct {
	/* FIXME -- locking */
	used	uint
	t	time.Time
	burst	uint
	eps	uint
	l	sync.Mutex
}

func (rl *RL)Put() {
	rl.l.Lock()
	if rl.used > 0 {
		rl.used--
	}
	rl.l.Unlock()
}

func (rl *RL)If() ([]uint) {
	return []uint{rl.used, rl.eps, rl.burst}
}

func (rl *RL)Get() bool {
	t := time.Now()
	d := t.Sub(rl.t)

	/* Handle enormous overflow first */
	rl.l.Lock()
	if d >= time.Second {
		rl.used = 0
		rl.t = t
	} else {
		/*
		 * Less than second passed. Let's calculate more
		 * carefully. Accumulated tokens = time * rate / 1 sec
		 */
		x := uint64(d) * uint64(rl.eps)
		if x >= uint64(time.Second) {
			acc := uint(x / uint64(time.Second))
			if acc >= rl.used {
				rl.used = 0
			} else {
				rl.used -= acc
			}
			rl.t = t
		}
	}

	if rl.used == rl.burst + 1 {
		rl.l.Unlock()
		return false
	}

	rl.used++
	rl.l.Unlock()
	return true
}

func (rl *RL)Update(burst, eps uint) {
	rl.burst = burst
	rl.eps = eps
}

func MakeRL(burst, eps uint) *RL {
	return &RL{t: time.Now(), burst: burst, eps: eps}
}
