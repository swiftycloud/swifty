package swyapi

/*
 * This type is not seen by wdog itself, instead, it's described
 * by each wdog runner by smth like "Request"
 */
type SwdFunctionRun struct {
	Event		string			`json:"event"`
	Args		map[string]string	`json:"args"`
	ContentType	string			`json:"content,omitempty"`
	Body		string			`json:"body,omitempty"`
	Claims		map[string]interface{}	`json:"claims,omitempty"` // JWT
	Method		string			`json:"method,omitempty"`
	Path		*string			`json:"path,omitempty"`
	Key		string			`json:"key,omitempty"`
	Src		*FunctionSources	`json:"src,omitempty"`
}

type SwdFunctionRunResult struct {
	Return		string		`json:"return"`
	Code		int		`json:"code"`
	Stdout		string		`json:"stdout"`
	Stderr		string		`json:"stderr"`
	Time		uint		`json:"time"` /* usec */
}

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
	Rate		uint	`json:"rate,omitempty",bson:"rate,omitempty"`
	Burst		uint	`json:"burst,omitempty",bson:"burst,omitempty"`
	MaxInProj	uint	`json:"maxinproj,omitempty",bson:"maxinproj,omitempty"`
	GBS		float64	`json:"gbs,omitempty",bson:"gbs,omitempty"`
	BytesOut	uint64	`json:"bytesout,omitempty",bson:"bytesout,omitempty"`
}

type UserLimits struct {
	UId	string			`json:"-",bson:"uid"`
	PlanId	string			`json:"planid",bson:"planid"`
	Fn	*FunctionLimits		`json:"function,omitempty",bson:"function,omitempty"`
}
