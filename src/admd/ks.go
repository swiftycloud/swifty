package main

import (
	"net/http"
	"fmt"
	"../apis/apps"
	"../common"
)

var ksToken string
var ksSwyDomainId string
var ksSwyOwnerRole string

func keystoneGetDomainId(conf *YAMLConfKeystone) (string, error) {
	var doms swy.KeystoneDomainsResp

	err := swy.KeystoneMakeReq(&swy.KeystoneReq {
			Type:	"GET",
			Addr:	conf.Addr,
			URL:	"domains",
			Token:	ksToken,
			Succ:	http.StatusOK, }, nil, &doms)
	if err != nil {
		return "", err
	}

	log.Debugf("Looking for domain %s", conf.Domain)
	for _, dom := range doms.Domains {
		if dom.Name == conf.Domain {
			log.Debugf("Found %s domain: %s", conf.Domain, dom.Id)
			return dom.Id, nil
		}
	}

	return "", fmt.Errorf("Can't find domain %s", conf.Domain)
}

func keystoneGetOwnerRoleId(conf *YAMLConfKeystone) (string, error) {
	var roles swy.KeystoneRolesResp

	err := swy.KeystoneMakeReq(&swy.KeystoneReq {
			Type:	"GET",
			Addr:	conf.Addr,
			URL:	"roles",
			Token:	ksToken,
			Succ:	http.StatusOK, }, nil, &roles)
	if err != nil {
		return "", err
	}

	log.Debugf("Looking for role %s", "swifty.owner")
	for _, role := range roles.Roles {
		if role.Name == swy.SwyUserRole {
			log.Debugf("Found role: %s", role.Id)
			return role.Id, nil
		}
	}

	return "", fmt.Errorf("Can't find swifty.owner role")
}

func ksListProjects(conf *YAMLConfKeystone) ([]string, error) {
	var projects swy.KeystoneProjectsResp
	var res []string

	err := swy.KeystoneMakeReq(&swy.KeystoneReq {
			Type:	"GET",
			Addr:	conf.Addr,
			URL:	"projects",
			Token:	ksToken,
			Succ:	http.StatusOK, }, nil, &projects)
	if err != nil {
		return res, err
	}

	for _, prj := range projects.Projects {
		if prj.DomainId != ksSwyDomainId {
			continue
		}

		res = append(res, prj.Name)
	}

	return res, nil
}

func ksAddUserAndProject(conf *YAMLConfKeystone, user *swyapi.AddUser) error {
	var presp swy.KeystoneProjectAdd

	err := swy.KeystoneMakeReq(
		&swy.KeystoneReq {
			Type:	"POST",
			Addr:	conf.Addr,
			URL:	"projects",
			Token:	ksToken,
			Succ:	http.StatusCreated, },
		&swy.KeystoneProjectAdd {
			Project: swy.KeystoneProject {
				Name: user.Id,
				DomainId: ksSwyDomainId,
			},
		}, &presp)

	if err != nil {
		return fmt.Errorf("Can't add KS project: %s", err.Error())
	}

	log.Debugf("Added project %s (id %s)", presp.Project.Name, presp.Project.Id[:8])

	var uresp swy.KeystonePassword

	err = swy.KeystoneMakeReq(
		&swy.KeystoneReq {
			Type:	"POST",
			Addr:	conf.Addr,
			URL:	"users",
			Token:	ksToken,
			Succ:	http.StatusCreated, },
		&swy.KeystonePassword {
			User: swy.KeystoneUser {
				Name: user.Id,
				Password: user.Pass,
				DefProject: presp.Project.Id,
				DomainId: ksSwyDomainId,
				Description: user.Name,
			},
		}, &uresp)
	if err != nil {
		return fmt.Errorf("Can't add KS user: %s", err.Error())
	}

	log.Debugf("Added user %s (id %s)", uresp.User.Name, uresp.User.Id[:8])

	err = swy.KeystoneMakeReq(&swy.KeystoneReq {
			Type:	"PUT",
			Addr:	conf.Addr,
			URL:	"projects/" + presp.Project.Id + "/users/" + uresp.User.Id + "/roles/" + ksSwyOwnerRole,
			Token:	ksToken,
			Succ:	http.StatusNoContent, }, nil, nil)
	if err != nil {
		return fmt.Errorf("Can't assign role: %s", err.Error())
	}

	return nil
}

func ksChangeUserPass(conf *YAMLConfKeystone, up *swyapi.UserLogin) error {
	var uresp swy.KeystoneUsersResp

	err := swy.KeystoneMakeReq(
		&swy.KeystoneReq {
			Type:	"GET",
			Addr:	conf.Addr,
			URL:	"users?name=" + up.UserName,
			Token:	ksToken,
			Succ:	http.StatusOK, },
		nil, &uresp)
	if err != nil {
		return err
	}
	if len(uresp.Users) != 1 {
		return fmt.Errorf("No such user: %s", up.UserName)
	}

	log.Debugf("Change pass for %s/%s", up.UserName, uresp.Users[0].Id)
	err = swy.KeystoneMakeReq(
		&swy.KeystoneReq {
			Type:	"PATCH",
			Addr:	conf.Addr,
			URL:	"users/" + uresp.Users[0].Id,
			Token:	ksToken,
			Succ:	http.StatusOK, },
		&swy.KeystonePassword {
			User: swy.KeystoneUser {
				Password: up.Password,
			},
		}, nil)
	if err != nil {
		return fmt.Errorf("Can't change password: %s", err.Error())
	}

	return nil
}

func ksInit(conf *YAMLConfKeystone) error {
	var err error

	log.Debugf("Logging in")
	ksToken, err = swy.KeystoneAuthWithPass(conf.Addr, "default",
				&swyapi.UserLogin{UserName: conf.Admin, Password: conf.Pass})
	if err != nil {
		return err
	}

	log.Debugf("Logged in as admin")
	ksSwyDomainId, err = keystoneGetDomainId(conf)
	if err != nil {
		return err
	}

	ksSwyOwnerRole, err = keystoneGetOwnerRoleId(conf)
	if err != nil {
		return err
	}

	return nil
}
