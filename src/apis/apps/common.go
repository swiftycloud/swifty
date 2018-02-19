package swyapi

type UserLogin struct {
	UserName	string			`json:"username"`
	Password	string			`json:"password"`
}

type UserToken struct {
	Expires		string			`json:"expires,omitempty"`
}

type PgRequest struct {
        Token   string  `json:"token"`
        User    string  `json:"user"`
        Pass    string  `json:"pass,omitempty"`
        DbName  string  `json:"dbname"`
}
