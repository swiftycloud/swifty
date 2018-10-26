package swyapi

import (
	"fmt"
	"net/http"
	"strings"
)

func url(url string, args []string) string {
	if len(args) != 0 {
		url += "?" + strings.Join(args, "&")
	}
	return url
}

func (cln *Client)Add(url string, succ int, in interface{}, out interface{}) {
	cln.Req1("POST", url, succ, in, out)
}

func (cln *Client)List(url string, succ int, out interface{}) {
	cln.Req1("GET", url, succ, nil, out)
}

func (cln *Client)Get(url string, succ int, out interface{}) {
	cln.Req1("GET", url, succ, nil, out)
}

func (cln *Client)Mod(url string, succ int, in interface{}) {
	cln.Req1("PUT", url, succ, in, nil)
}

func (cln *Client)Del(url string, succ int) {
	cln.Req1("DELETE", url, succ, nil, nil)
}

type Collection struct {
	cln	*Client
	pref	string
}

func (c *Collection)Add(in, out interface{}) {
	c.cln.Add(c.pref, http.StatusOK, in, out)
}

func (c *Collection)List(q []string, out interface{}) {
	c.cln.List(url(c.pref, q), http.StatusOK, out)
}

func (c *Collection)Get(id string, out interface{}) {
	c.cln.Get(c.pref + "/" + id, http.StatusOK, out)
}

func (c *Collection)Del(id string) {
	c.cln.Del(c.pref + "/" + id, http.StatusOK)
}

func (c *Collection)Prop(id string, pn string, out interface{}) {
	c.cln.Get(c.pref + "/" + id + "/" + pn, http.StatusOK, out)
}

func (c *Collection)Set(id string, pn string, in interface{}) {
	sfx := "/" + id
	if pn != "" {
		sfx += "/" + pn
	}
	c.cln.Mod(c.pref + sfx, http.StatusOK, in)
}

func (c *Collection)sub(id, name string) *Collection {
	return &Collection{c.cln, c.pref + "/" + id + "/" + name}
}

func (cln *Client)Functions() *Collection {
	return &Collection{cln, "functions"}
}

func (cln *Client)Mwares() *Collection {
	return &Collection{cln, "middleware"}
}

func (cln *Client)Deployments() *Collection {
	return &Collection{cln, "deployments"}
}

func (cln *Client)Routers() *Collection {
	return &Collection{cln, "routers"}
}

func (cln *Client)Accounts() *Collection {
	return &Collection{cln, "accounts"}
}

func (cln *Client)Packages(lng string) *Collection {
	return &Collection{cln, "packages/" + lng}
}

func (cln *Client)Repos() *Collection {
	return &Collection{cln, "repos"}
}

func (cln *Client)Triggers(fid string) *Collection {
	return cln.Functions().sub(fid, "triggers")
}

func (c *Collection)Resolve(proj, name string) (string, bool) {
	if strings.HasPrefix(name, ":") {
		return name[1:], false
	}

	ua := []string{}
	if proj != "" {
		ua = append(ua, "project=" + proj)
	}

	var objs []map[string]interface{}
	ua = append(ua, "name=" + name)

	c.List(ua, &objs)

	for _, obj := range objs {
		if obj["name"] == name {
			return obj["id"].(string), true
		}
	}

	if c.cln.onerr != nil {
		c.cln.onerr(fmt.Errorf("\tname %s not resolved", name))
	}
	return "", false
}

