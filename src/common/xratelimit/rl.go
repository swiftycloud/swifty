package xratelimit

import (
	"time"
)

type RL struct {
	/* FIXME -- locking */
	used	uint
	t	time.Time
	burst	uint
	eps	uint
}

func (rl *RL)Put() {
	if rl.used > 0 {
		rl.used--
	}
}

func (rl *RL)Get() bool {
	t := time.Now()
	d := t.Sub(rl.t)

	/* Handle enormous overflow first */
	if d >= time.Second {
		rl.used = 0
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
		return false
	}

	rl.used++
	return true
}

func (rl *RL)Update(burst, eps uint) {
	rl.burst = burst
	rl.eps = eps
}

func MakeRL(burst, eps uint) *RL {
	return &RL{t: time.Now(), burst: burst, eps: eps}
}
