package s3mgo

import (
	"encoding/binary"
	"fmt"
	"time"
	"reflect"
)

func (v ActionBits) ToMgo() ActionBitsMgo {
	var b ActionBitsMgo

	binary.LittleEndian.PutUint64(b[0:], v[0])
	binary.LittleEndian.PutUint64(b[8:], v[1])

	return b
}

func (v ActionBitsMgo) ToSwy() ActionBits {
	var b ActionBits

	b[0] = binary.LittleEndian.Uint64(v[0:])
	b[1] = binary.LittleEndian.Uint64(v[8:])

	return b
}

func (policy *S3Policy) InfoLong() string {
	if policy != nil {
		if len(policy.Resource) > 0 {
			return fmt.Sprintf("% x/%s",
				policy.Action.ToSwy(),
				policy.Resource[0])
		}
	}
	return "nil"
}

func (policy *S3Policy) Equal(dst *S3Policy) bool {
	return reflect.DeepEqual(policy, dst)
}

func (policy *S3Policy) MayAccess(resource string) bool {
	if len(policy.Resource) > 0 && policy.Resource[0] == Resourse_Any {
		return true
	}

	for _, x := range policy.Resource {
		if x == resource {
			return true
		}
	}

	return false
}

func (policy *S3Policy) Allowed(action int) bool {
	bits := policy.Action.ToSwy()
	if action < 64 {
		return bits[0] & (1 << uint(action)) != 0
	} else {
		return bits[1] & (1 << uint((action - 64))) != 0
	}
}

func now() int64 {
	return time.Now().Unix()
}

func (akey *S3AccessKey) Expired() bool {
	if akey.ExpirationTimestamp < S3TimeStampMax {
		return now() > akey.ExpirationTimestamp
	}
	return false
}
