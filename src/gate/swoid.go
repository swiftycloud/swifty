package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

type SwoId struct {
	Tennant		string		`bson:"tennant"`
	Project		string		`bson:"project"`
	Name		string		`bson:"name"`
}

func makeSwoId(tennant, project, name string) *SwoId {
	if project == "" {
		project = DefaultProject
	}

	return &SwoId{Tennant: tennant, Project: project, Name: name}
}

func ctxSwoId(ctx context.Context, project, name string) *SwoId {
	return makeSwoId(gctx(ctx).Tenant, project, name)
}

func (id *SwoId)NameOK() bool {
	for _, c := range id.Name {
		if (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '_' || c == '.' {
				continue
			}

		return false
	}

	return true
}

func (id *SwoId) Str() string {
	rv := id.Tennant
	if id.Project != "" {
		rv += "/" + id.Project
		if id.Name != "" {
			rv += "/" + id.Name
		}
	}
	return rv
}

func cookify(val string) string {
	h := sha256.New()
	h.Write([]byte(val))
	return hex.EncodeToString(h.Sum(nil))
}

func cookifyS(vals ...string) string {
	h := sha256.New()
	for _, v := range vals {
		h.Write([]byte(v + "::"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (id *SwoId) Cookie() string {
	return cookify(id.Tennant + "/" + id.Project + "/" + id.Name)
}

func (id *SwoId) Cookie2(salt string) string {
	return cookify(salt + ":" + id.Tennant + "/" + id.Project + "/" + id.Name)
}

func (id *SwoId) Namespace() string {
	return cookify(id.Tennant + "/" + id.Project)
}
