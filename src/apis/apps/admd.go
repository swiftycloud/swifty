package swyapi

type UserInfo struct {
	ID	string		`json:"id"`
	UId	string		`json:"uid"`
	Name	string		`json:"name,omitempty"`
	Created	string		`json:"created,omitempty"`
	Roles	[]string	`json:"roles,omitempty"`
}

type AddUser struct {
	UId	string		`json:"uid"`
	Pass	string		`json:"pass"`
	Name	string		`json:"name"`
	PlanId	string		`json:"planid"`
}
