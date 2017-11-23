package swyapi

type ListUsers struct {
}

type UserInfo struct {
	Id	string		`json:"id"`
	Name	string		`json:"name"`
}

type AddUser struct {
	Id	string		`json:"id"`
	Pass	string		`json:"pass"`
	Name	string		`json:"name"`
}
