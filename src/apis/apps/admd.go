package swyapi

type ListUsers struct {
}

type UserInfo struct {
	Id	string		`json:"id"`
	Name	string		`json:"name,omitempty"`
}

type AddUser struct {
	Id	string		`json:"id"`
	Pass	string		`json:"pass"`
	Name	string		`json:"name"`
	PlanId	string		`json:"planid"`
}
