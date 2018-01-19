package main

import (
	"net/http"
	"fmt"
	"../apis/apps"
	"../common/keystone"
)

var ksClient *swyks.KsClient
var ksSwyDomainId string
var ksSwyOwnerRole string

func keystoneGetDomainId(conf *YAMLConfKeystone) (string, error) {
	var doms swyks.KeystoneDomainsResp

	err := ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"domains",
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
	var roles swyks.KeystoneRolesResp

	err := ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"roles",
			Succ:	http.StatusOK, }, nil, &roles)
	if err != nil {
		return "", err
	}

	log.Debugf("Looking for role %s", "swifty.owner")
	for _, role := range roles.Roles {
		if role.Name == swyks.SwyUserRole {
			log.Debugf("Found role: %s", role.Id)
			return role.Id, nil
		}
	}

	return "", fmt.Errorf("Can't find swifty.owner role")
}

func ksListUsers(conf *YAMLConfKeystone) (*[]swyapi.UserInfo, error) {
	var users swyks.KeystoneUsersResp
	var res []swyapi.UserInfo

	err := ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"users",
			Succ:	http.StatusOK, }, nil, &users)
	if err != nil {
		return nil, err
	}

	for _, usr := range users.Users {
		if usr.DomainId != ksSwyDomainId {
			continue
		}

		res = append(res, swyapi.UserInfo{Id: usr.Name, Name: usr.Description})
	}

	return &res, nil
}

func ksAddUserAndProject(conf *YAMLConfKeystone, user *swyapi.AddUser) error {
	var presp swyks.KeystoneProjectAdd

	err := ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"POST",
			URL:	"projects",
			Succ:	http.StatusCreated, },
		&swyks.KeystoneProjectAdd {
			Project: swyks.KeystoneProject {
				Name: user.Id,
				DomainId: ksSwyDomainId,
			},
		}, &presp)

	if err != nil {
		return fmt.Errorf("Can't add KS project: %s", err.Error())
	}

	log.Debugf("Added project %s (id %s)", presp.Project.Name, presp.Project.Id[:8])

	var uresp swyks.KeystonePassword

	err = ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"POST",
			URL:	"users",
			Succ:	http.StatusCreated, },
		&swyks.KeystonePassword {
			User: swyks.KeystoneUser {
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

	err = ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"PUT",
			URL:	"projects/" + presp.Project.Id + "/users/" + uresp.User.Id + "/roles/" + ksSwyOwnerRole,
			Succ:	http.StatusNoContent, }, nil, nil)
	if err != nil {
		return fmt.Errorf("Can't assign role: %s", err.Error())
	}

	return nil
}

func ksGetUserInfo(conf *YAMLConfKeystone, user string) (*swyks.KeystoneUser, error) {
	var uresp swyks.KeystoneUsersResp

	err := ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"users?name=" + user,
			Succ:	http.StatusOK, },
		nil, &uresp)
	if err != nil {
		return nil, err
	}
	if len(uresp.Users) != 1 {
		return nil, fmt.Errorf("No such user: %s", user)
	}

	return &uresp.Users[0], nil
}

func ksGetProjectInfo(conf *YAMLConfKeystone, project string) (*swyks.KeystoneProject, error) {
	var presp swyks.KeystoneProjectsResp

	err := ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"projects?name=" + project,
			Succ:	http.StatusOK, },
		nil, &presp)
	if err != nil {
		return nil, err
	}
	if len(presp.Projects) != 1 {
		return nil, fmt.Errorf("No such project: %s", project)
	}

	return &presp.Projects[0], nil
}

func ksChangeUserPass(conf *YAMLConfKeystone, up *swyapi.UserLogin) error {
	uinf, err := ksGetUserInfo(conf, up.UserName)
	if err != nil {
		return err
	}

	log.Debugf("Change pass for %s/%s", up.UserName, uinf.Id)
	err = ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"PATCH",
			URL:	"users/" + uinf.Id,
			Succ:	http.StatusOK, },
		&swyks.KeystonePassword {
			User: swyks.KeystoneUser {
				Password: up.Password,
			},
		}, nil)
	if err != nil {
		return fmt.Errorf("Can't change password: %s", err.Error())
	}

	return nil
}

func ksDelUserAndProject(conf *YAMLConfKeystone, ui *swyapi.UserInfo) error {
	var err error

	uinf, err := ksGetUserInfo(conf, ui.Id)
	if err != nil {
		return err
	}

	err = ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"DELETE",
			URL:	"users/" + uinf.Id,
			Succ:	http.StatusNoContent, }, nil, nil)
	if err != nil {
		return err
	}

	pinf, err := ksGetProjectInfo(conf, ui.Id)
	if err != nil {
		return err
	}

	err = ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"DELETE",
			URL:	"projects/" + pinf.Id,
			Succ:	http.StatusNoContent, }, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func ksInit(conf *YAMLConfKeystone) error {
	var err error

	log.Debugf("Logging in")
	ksClient, err = swyks.KeystoneConnect(conf.Addr, "default",
				&swyapi.UserLogin{UserName: conf.Admin, Password: admdSecrets[conf.Pass]})
	if err != nil {
		return err
	}

	log.Debugf("Logged in as admin")
	ksSwyDomainId, err = keystoneGetDomainId(conf)
	if err != nil {
		return fmt.Errorf("Can't get domain: %s", err.Error())
	}

	ksSwyOwnerRole, err = keystoneGetOwnerRoleId(conf)
	if err != nil {
		return fmt.Errorf("Can't get role: %s", err.Error())
	}

	return nil
}
