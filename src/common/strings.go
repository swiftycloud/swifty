package xh

import (
	"strings"
)

type StringsValues map[string]bool

func MakeStringValues(vals ...string) StringsValues {
	ret := make(map[string]bool)
	for _, v := range(vals) {
		ret[v] = true
	}
	return ret
}

func ParseStringValues(vals_sep string) StringsValues {
	return MakeStringValues(strings.Split(vals_sep, ":")...)
}

func (sv StringsValues)String() string {
	ret := ""
	for k, _ := range(sv) {
		ret += ":" + k
	}

	return ret[1:]
}

func (sv StringsValues)Have(v string) bool {
	_, ok := sv[v]
	return ok
}
