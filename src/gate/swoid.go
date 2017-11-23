package main

import (
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
		project = SwyDefaultProject
	}

	return &SwoId{Tennant: tennant, Project: project, Name: name}
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
	h := sha256.New()
	h.Write([]byte(id.Tennant + "/" + id.Project + "/" + id.Name))
	return hex.EncodeToString(h.Sum(nil))
}
