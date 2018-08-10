package main

import (
	"net/http"
	"fmt"
	"encoding/json"
	"../apis/apps"
	"../common"
	"../common/keystone"
	"time"
)

type UserDesc struct {
	Name	string		`json:"name"`
	Email	string		`json:"email"`
	Created	*time.Time	`json:"created,omitempty"`
}

func (kud *UserDesc)CreatedS() string {
	if kud.Created != nil {
		return kud.Created.Format(time.RFC1123Z)
	} else {
		return ""
	}
}

var ksClient *swyks.KsClient
var ksSwyDomainId string
var ksSwyOwnerRole string
var ksSwyAdminRole string

func keystoneGetDomainId(c *swy.XCreds) (string, error) {
	var doms swyks.KeystoneDomainsResp

	err := ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"domains",
			Succ:	http.StatusOK, }, nil, &doms)
	if err != nil {
		return "", err
	}

	log.Debugf("Looking for domain %s", c.Domn)
	for _, dom := range doms.Domains {
		if dom.Name == c.Domn {
			log.Debugf("Found %s domain: %s", c.Domn, dom.Id)
			return dom.Id, nil
		}
	}

	return "", fmt.Errorf("Can't find domain %s", c.Domn)
}

func keystoneGetRolesId(c *swy.XCreds) (string, string, error) {
	var roles swyks.KeystoneRolesResp

	err := ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"roles",
			Succ:	http.StatusOK, }, nil, &roles)
	if err != nil {
		return "", "", err
	}

	var or, ar string

	log.Debugf("Looking for roles %s, %s", swyks.SwyUserRole, swyks.SwyAdminRole)
	for _, role := range roles.Roles {
		if role.Name == swyks.SwyUserRole {
			log.Debugf("Found user role: %s", role.Id)
			or = role.Id
			continue
		}
		if role.Name == swyks.SwyAdminRole {
			log.Debugf("Found admin role: %s", role.Id)
			ar = role.Id
			continue
		}
	}

	if or == "" || ar == "" {
		return "", "", fmt.Errorf("Can't find swifty.owner/.admin role")
	}

	return or, ar, nil
}

func listUsers(c *swy.XCreds) ([]*swyapi.UserInfo, error) {
	var users swyks.KeystoneUsersResp
	var res []*swyapi.UserInfo

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

		ui, err := toUserInfo(&usr)
		if err != nil {
			return nil, err
		}

		res = append(res, ui)
	}

	return res, nil
}

func ksAddUserAndProject(c *swy.XCreds, user *swyapi.AddUser) (string, error) {
	var presp swyks.KeystoneProjectAdd

	now := time.Now()
	udesc, err := json.Marshal(&UserDesc{
		Name:		user.Name,
		Email:		user.UId,
		Created:	&now,
	})
	if err != nil {
		return "", fmt.Errorf("Can't marshal user desc: %s", err.Error())
	}

	err = ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"POST",
			URL:	"projects",
			Succ:	http.StatusCreated, },
		&swyks.KeystoneProjectAdd {
			Project: swyks.KeystoneProject {
				Name: user.UId,
				DomainId: ksSwyDomainId,
			},
		}, &presp)

	if err != nil {
		return "", fmt.Errorf("Can't add KS project: %s", err.Error())
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
				Name: user.UId,
				Password: user.Pass,
				DefProject: presp.Project.Id,
				DomainId: ksSwyDomainId,
				Description: string(udesc),
			},
		}, &uresp)
	if err != nil {
		return "", fmt.Errorf("Can't add KS user: %s", err.Error())
	}

	log.Debugf("Added user %s (id %s)", uresp.User.Name, uresp.User.Id[:8])

	err = ksClient.MakeReq(&swyks.KeystoneReq {
			Type:	"PUT",
			URL:	"projects/" + presp.Project.Id + "/users/" + uresp.User.Id + "/roles/" + ksSwyOwnerRole,
			Succ:	http.StatusNoContent, }, nil, nil)
	if err != nil {
		return "", fmt.Errorf("Can't assign role: %s", err.Error())
	}

	return uresp.User.Id, nil
}

func toUserDesc(ui *swyks.KeystoneUser) (*UserDesc, error) {
	var kud UserDesc
	var err error
	if ui.Description != "" {
		err = json.Unmarshal([]byte(ui.Description), &kud)
		if err != nil {
			log.Errorf("Unmarshal [%s] error: %s", ui.Description, err.Error())
		}
	}
	return &kud, err
}

func toUserInfo(ui *swyks.KeystoneUser) (*swyapi.UserInfo, error) {
	kud, err := toUserDesc(ui)
	if err != nil {
		return nil, err
	}

	return &swyapi.UserInfo {
		ID:	 ui.Id,
		UId:	 ui.Name,
		Name:	 kud.Name,
		Created: kud.CreatedS(),
	}, nil
}

func getUserInfo(c *swy.XCreds, user string) (*swyapi.UserInfo, error) {
	kui, err := ksGetUserInfo(c, user)
	if err != nil {
		return nil, err
	}

	krs, err := ksGetUserRoles(c, kui)
	if err != nil {
		return nil, err
	}

	var ui *swyapi.UserInfo

	ui, err = toUserInfo(kui)
	if err != nil {
		return nil, fmt.Errorf("Can't unmarshal user desc: %s", err.Error())
	}

	for _, role := range(krs) {
		ui.Roles = append(ui.Roles, role.Name)
	}

	return ui, nil
}

func ksGetUserRoles(c *swy.XCreds, ui *swyks.KeystoneUser) ([]*swyks.KeystoneRole, error) {
	var rass swyks.KeystoneRoleAssignments

	err := ksClient.MakeReq(
		&swyks.KeystoneReq {
			Type:	"GET",
			URL:	"role_assignments?include_names&user.id=" + ui.Id,
			Succ:	http.StatusOK, },
		nil, &rass)
	if err != nil {
		return nil, err
	}

	var ret []*swyks.KeystoneRole
	for _, ra := range(rass.Ass) {
		ret = append(ret, &ra.Role)
	}

	return ret, nil
}

func ksGetUserInfo(c *swy.XCreds, user string) (*swyks.KeystoneUser, error) {
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

func ksGetProjectInfo(c *swy.XCreds, project string) (*swyks.KeystoneProject, error) {
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

func ksChangeUserPass(c *swy.XCreds, up *swyapi.UserLogin) error {
	uinf, err := ksGetUserInfo(c, up.UserName)
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

func ksDelUserAndProject(c *swy.XCreds, ui *swyapi.UserInfo) error {
	var err error

	uinf, err := ksGetUserInfo(c, ui.UId)
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

	pinf, err := ksGetProjectInfo(c, ui.UId)
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

func ksInit(c *swy.XCreds) error {
	var err error

	log.Debugf("Logging in")
	ksClient, err = swyks.KeystoneConnect(c.Addr(), "default",
				&swyapi.UserLogin{UserName: c.User, Password: admdSecrets[c.Pass]})
	if err != nil {
		return err
	}

	log.Debugf("Logged in as admin [%s]", ksClient.Token)
	ksSwyDomainId, err = keystoneGetDomainId(c)
	if err != nil {
		return fmt.Errorf("Can't get domain: %s", err.Error())
	}

	ksSwyOwnerRole, ksSwyAdminRole, err = keystoneGetRolesId(c)
	if err != nil {
		return fmt.Errorf("Can't get role: %s", err.Error())
	}

	return nil
}
