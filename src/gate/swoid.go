package main

import (
	"context"
	"../common"
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

func (id *SwoId) Cookie() string {
	return xh.Cookify(id.Tennant + "/" + id.Project + "/" + id.Name)
}

func (id *SwoId) Cookie2(salt string) string {
	return xh.Cookify(salt + ":" + id.Tennant + "/" + id.Project + "/" + id.Name)
}

func (id *SwoId) S3Namespace() string {
	return xh.Cookify(id.Tennant + "/" + DefaultProject)
}
