package swyapi

type ListUsers struct {
}

type UserInfo struct {
	Id	string		`json:"id"`
}

type AddUser struct {
	Id	string		`json:"id"`
	Pass	string		`json:"pass"`
}
