package xh

import (
	"strings"
	"net"
)

type XCreds struct {
	User    string
	Pass    string
	Host    string
	Port    string
	Domn	string
}

func (xc *XCreds)Addr() string {
	return xc.Host + ":" + xc.Port
}

func (xc *XCreds)AddrP(port string) string {
	return xc.Host + ":" + port
}

func (xc *XCreds)URL() string {
	s := xc.User + ":" + xc.Pass + "@" + xc.Host + ":" + xc.Port
	if xc.Domn != "" {
		s += "/" + xc.Domn
	}
	return s
}

func (xc *XCreds)Resolve() {
	if net.ParseIP(xc.Host) == nil {
		ips, err := net.LookupIP(xc.Host)
		if err == nil && len(ips) > 0 {
			xc.Host = ips[0].String()
		}
	}
}

func ParseXCreds(url string) *XCreds {
	xc := &XCreds{}
	/* user:pass@host:port */
	x := strings.SplitN(url, ":", 2)
	xc.User = x[0]
	x = strings.SplitN(x[1], "@", 2)
	xc.Pass = x[0]
	x = strings.SplitN(x[1], ":", 2)
	xc.Host = x[0]
	x = strings.SplitN(x[1], "/", 2)
	xc.Port = x[0]
	if len(x) > 1 {
		xc.Domn = x[1]
	}

	return xc
}

