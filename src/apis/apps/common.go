package swyapi

type UserLogin struct {
	UserName	string			`json:"username"`
	Password	string			`json:"password"`
}

type UserToken struct {
	Endpoint	string			`json:"endpoint"`
	Expires		string			`json:"expires,omitempty"`
}

type PgRequest struct {
        Token   string  `json:"token"`
        User    string  `json:"user"`
        Pass    string  `json:"pass,omitempty"`
        DbName  string  `json:"dbname"`
}

type FunctionLimits struct {
	Rate	uint		`json:"rate,omitempty",bson:"rate,omitempty"`
	Burst	uint		`json:"burst,omitempty",bson:"burst,omitempty"`
}

type UserLimits struct {
	Id	string			`json:"id",bson:"id"`
	Fn	*FunctionLimits		`json:"function,omitempty",bson:"function,omitempty"`
}
