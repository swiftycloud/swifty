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
}

type FunctionLimits struct {
	Rate	uint		`json:"rate,omitempty",bson:"rate,omitempty"`
	Burst	uint		`json:"burst,omitempty",bson:"burst,omitempty"`
}

type UserLimits struct {
	Id	string			`json:"id",bson:"id"`
	Fn	*FunctionLimits		`json:"function,omitempty",bson:"function,omitempty"`
}
