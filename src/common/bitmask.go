/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xh

type Bitmask uint32

func b2m(bit uint) Bitmask { return 1 << bit }
func (b *Bitmask)Set(bit uint) { *b |= b2m(bit) }
func (b *Bitmask)Test(bit uint) bool { return *b & b2m(bit) != 0 }
func (b *Bitmask)Fill() { *b = 0xFFFFFFFF }
