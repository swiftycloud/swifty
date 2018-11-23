/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xrl

import (
	"time"
	"sync"
)

type RL struct {
	bts	uint
	t	time.Time
	eps	uint
	burst	uint
	base	time.Duration
	l	sync.Mutex
}

func (rl *RL)Put() {
	rl.l.Lock()
	if rl.bts < rl.burst {
		rl.bts++
	}
	rl.l.Unlock()
}

func (rl *RL)If() ([]uint) {
	return []uint{rl.bts, rl.eps, rl.burst}
}

func (rl *RL)Get() bool {
	rl.l.Lock()
	if rl.bts == 0 {
		t := time.Now()
		d := t.Sub(rl.t)
		if d >= rl.base {
			rl.bts = rl.burst
			rl.t = t
		} else {
			/* time.Second / rl.eps time is needed to get one bts */
			nb := uint(uint64(d) * uint64(rl.eps) / uint64(rl.base))
			if nb == 0 {
				rl.l.Unlock()
				return false
			}

			if nb > rl.burst {
				rl.bts = rl.burst
			} else {
				rl.bts = nb
			}

			rl.t = rl.t.Add(rl.base * time.Duration(nb) / time.Duration(rl.eps))
		}
	}

	rl.bts--
	rl.l.Unlock()
	return true
}

func (rl *RL)Update(burst, eps uint) {
	rl.l.Lock()
	rl.burst = burst + 1
	rl.bts = rl.burst
	rl.eps = eps
	rl.t = time.Now()
	rl.l.Unlock()
}

func MakeRLBase(burst, eps uint, base time.Duration) *RL {
	return &RL{t: time.Now(), bts: burst + 1, burst: burst + 1, eps: eps, base: base}
}

func MakeRL(burst, eps uint) *RL {
	return MakeRLBase(burst, eps, time.Second)
}
