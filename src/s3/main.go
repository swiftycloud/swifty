/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"go.uber.org/zap"
	"gopkg.in/mgo.v2"
	"github.com/gorilla/mux"

	"encoding/hex"
	"net/http"
	"context"
	"strings"
	"errors"
	"time"
	"flag"
	"fmt"
	"os"

	"swifty/s3/mgo"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/secrets"
	"swifty/common/xrest/sysctl"
	"swifty/apis/s3"
)

var s3Secrets xsecret.Store
var s3SecKey []byte
var S3ModeDevel bool

func isLite() bool { return Flavor == "lite" }

type YAMLConfCeph struct {
	ConfigPath	string			`yaml:"config-path"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	WebPort		string			`yaml:"webport"`
	Token		string			`yaml:"token"`
	LogLevel	string			`yaml:"loglevel"`
	Prometheus	string			`yaml:"prometheus"`
	HTTPS		*xhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConfNotify struct {
	Rabbit		string			`yaml:"rabbitmq,omitempty"`
}

type YAMLConf struct {
	DB		string			`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Ceph		YAMLConfCeph		`yaml:"ceph"`
	SecKey		string			`yaml:"secretskey"`
	Notify		YAMLConfNotify		`yaml:"notify"`
	Mimes		string			`yaml:"mime-types"`
}

var conf YAMLConf
var log *zap.SugaredLogger
var adminsrv *http.Server
var gatesrv *http.Server

func setupLogger(conf *YAMLConf) {
	lvl := zap.WarnLevel

	if conf != nil {
		switch conf.Daemon.LogLevel {
		case "debug":
			lvl = zap.DebugLevel
			break
		case "info":
			lvl = zap.InfoLevel
			break
		case "warn":
			lvl = zap.WarnLevel
			break
		case "error":
			lvl = zap.ErrorLevel
			break
		}
	}

	zcfg := zap.Config {
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      true,
		DisableStacktrace:true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, _ := zcfg.Build()
	log = logger.Sugar()
}

type s3Context struct {
	context.Context
	id	string
	S	*mgo.Session
	errCode	int
	mime	string
	iam	*s3mgo.Iam
}

func Dbs(ctx context.Context) *mgo.Session {
	return ctx.(*s3Context).S
}

func ctxAuthorize(ctx context.Context, iam *s3mgo.Iam) {
	ctx.(*s3Context).iam = iam
}

func ctxIam(ctx context.Context) *s3mgo.Iam {
	return ctx.(*s3Context).iam
}

func ctxAllowed(ctx context.Context, action int) bool {
	return ctxIam(ctx).Policy.Allowed(action)
}

func ctxMayAccess(ctx context.Context, bname string) bool {
	return ctxIam(ctx).Policy.MayAccess(bname)
}

func mkContext(id string) (context.Context, func(context.Context)) {
	ctx := &s3Context{
		context.Background(), id,
		session.Copy(),
		0, "", nil,
	}

	return ctx, func(c context.Context) {
				Dbs(c).Close()
			}
}

var CORS_Headers = []string {
	swys3api.SwyS3_AccessKey,
	swys3api.SwyS3_AdminToken,
	"Content-Type",
	"Content-Length",
	"Content-MD5",
	"Authorization",
	"X-Amz-Date",
	"x-amz-acl",
	"x-amz-copy-source",
	"X-Amz-Content-Sha256",
	"X-Amz-User-Agent",
}

var CORS_Methods = []string {
	http.MethodPost,
	http.MethodPut,
	http.MethodDelete,
	http.MethodGet,
	http.MethodHead,
}

var logReqDetails int = 1

func logRequest(r *http.Request) {
	var request []string

	if logReqDetails == 0 {
		return
	}

	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, "\n---")
	request = append(request, url)
	request = append(request, fmt.Sprintf("Host: %v", r.Host))

	if logReqDetails >= 2 {
		for name, headers := range r.Header {
			for _, h := range headers {
				request = append(request, fmt.Sprintf("%v:%v", name, h))
			}
		}

		content_type := r.Header.Get("Content-Type")
		if content_type != "" {
			if len(content_type) >= 21 &&
			string(content_type[0:20]) == "multipart/form-data;" {
				r.ParseMultipartForm(0)
				request = append(request, fmt.Sprintf("MultipartForm: %v", r.MultipartForm))
			}
		}
	}

	request = append(request, "---")

	log.Debug(strings.Join(request, "\n"))
}

func s3AuthorizeGetKey(ctx context.Context, r *http.Request) (*s3mgo.AccessKey, int, error) {
	akey, code, err := s3AuthorizeUser(ctx, r)
	if akey != nil || err != nil {
		return akey, code, err
	}

	akey, err = s3AuthorizeAdmin(ctx, r)
	if akey != nil || err != nil {
		return akey, S3ErrAccessDenied, err
	}

	return nil, S3ErrAccessDenied, errors.New("Not authorized")
}

func s3Authorize(ctx context.Context, r *http.Request) (int, error) {
	key, code, err := s3AuthorizeGetKey(ctx, r)
	if err != nil {
		return code, err
	}

	if key.Expired() {
		return S3ErrAccessDenied, errors.New("Key is expired")
	}

	iam, err := s3IamFind(ctx, key)
	if err == nil {
		log.Debugf("Authorized user, key %s", key.AccessKeyID)
	}

	ctxAuthorize(ctx, iam)
	return 0, nil
}

func handleS3API(cb func(ctx context.Context, w http.ResponseWriter, r *http.Request) *S3Error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		ctx, done := mkContext("api")
		defer done(ctx)

		logRequest(r)

		if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

		code, err := s3Authorize(ctx, r)
		if err != nil {
			HTTPRespError(w, code, err.Error())
			return
		}

		if e := cb(ctx, w, r); e != nil {
			HTTPRespS3Error(w, e)
		}
	})
}

func makeAdminURL(clienturl, admport string) string {
	return strings.Split(clienturl, ":")[0] + ":" + admport
}

func main() {
	var config_path string
	var showVersion bool
	var err error

	flag.BoolVar(&S3ModeDevel, "devel", false, "launch in development mode")
	flag.StringVar(&config_path,
			"conf",
				"/etc/swifty/conf/s3.yaml",
				"path to a config file")
	flag.BoolVar(&radosDisabled,
			"no-rados",
				false,
				"disable rados")
	flag.BoolVar(&showVersion,
			"version",
				false,
				"show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("Version %s\n", Version)
		return
	}

	if _, err := os.Stat(config_path); err == nil {
		xh.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	log.Debugf("config: %v", &conf)

	s3Secrets, err = xsecret.Init("s3")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	conf.SecKey, err = s3Secrets.Get(conf.SecKey)
	if err != nil {
		log.Error("Cannot find seckey secret: %s", err.Error())
		return
	}

	s3SecKey, err = hex.DecodeString(conf.SecKey)
	if err != nil || len(s3SecKey) < 16 {
		log.Error("Secret key should be decodable and be 16 bytes long at least")
		return
	}

	adminAccToken, err = s3Secrets.Get(conf.Daemon.Token)
	if err != nil || len(adminAccToken) < 16 {
		log.Debugf(">> %s vs %s", conf.Daemon.Token, adminAccToken)
		log.Errorf("Bad admin access token: %s", err)
		return
	}

	err = webReadMimes(conf.Mimes)
	if err != nil {
		log.Error("Cannot read mime-types: %s", err.Error())
		return
	}

	sysctl.AddIntSysctl("s3_req_verb", &logReqDetails)
	sysctl.AddRoSysctl("s3_version", func() string { return Version })
	sysctl.AddRoSysctl("s3_mode", func() string {
		ret := "mode:"
		if S3ModeDevel {
			ret += "devel"
		} else {
			ret += "prod"
		}

		ret += ", flavor:" + Flavor
		ret += ", rados:"
		if radosDisabled {
			ret += "no"
		} else {
			ret += "yes"
		}

		return ret
	})


	// Service operations
	rgatesrv := mux.NewRouter()
	match_bucket := fmt.Sprintf("/{BucketName:%s*}",
		S3BucketName_Letter)
	match_object := fmt.Sprintf("/{BucketName:%s+}/{ObjName:%s*}",
		S3BucketName_Letter, S3ObjectName_Letter)

	rgatesrv.Handle(match_bucket,	handleS3API(handleBucket))
	rgatesrv.Handle(match_object,	handleS3API(handleObjectReq))

	// Web server operations
	rwebsrv := mux.NewRouter()
	rwebsrv.Methods("GET", "HEAD").HandlerFunc(handleWebReq)
	if conf.Daemon.WebPort == "" {
		conf.Daemon.WebPort = "8080"
	}

	// Admin operations
	radminsrv := mux.NewRouter()
	radminsrv.HandleFunc("/v1/api/keys", handleKeys).Methods("POST", "DELETE")
	radminsrv.HandleFunc("/v1/api/notify", handleNotify).Methods("POST", "DELETE")
	radminsrv.HandleFunc("/v1/api/stats/{ns}", handleStats).Methods("GET")
	radminsrv.HandleFunc("/v1/api/stats/{ns}/limits", handleLimits).Methods("PUT")
	radminsrv.HandleFunc("/v1/sysctl", handleSysctls).Methods("GET", "OPTIONS")
	radminsrv.HandleFunc("/v1/sysctl/{name}", handleSysctl).Methods("GET", "PUT", "OPTIONS")

	err = dbConnect(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to backend: %s",
				err.Error())
	}

	ctx, done := mkContext("init")

	err = radosInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to rados: %s",
				err.Error())
	}

	err = notifyInit(&conf.Notify)
	if err != nil {
		log.Fatalf("Can't setup notifications: %s", err.Error())
	}

	err = dbRepair(ctx)
	if err != nil {
		log.Fatalf("Can't process db test/repair: %s", err.Error())
	}

	err = gcInit(0)
	if err != nil {
		log.Fatalf("Can't setup garbage collector: %s", err.Error())
	}

	err = PrometheusInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup prometheus: %s", err.Error())
	}

	done(ctx)

	go func() {
		adminsrv = &http.Server{
			Handler:      radminsrv,
			Addr:         makeAdminURL(conf.Daemon.Addr, conf.Daemon.AdminPort),
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}

		err = adminsrv.ListenAndServe()
		if err != nil {
			log.Errorf("ListenAndServe: adminsrv %s", err.Error())
		}
	}()

	go func() {
		webRoot = strings.SplitN(conf.Daemon.Addr, ":", 2)[0]
		websrv := &http.Server{
			Handler:	rwebsrv,
			Addr:		webRoot + ":" + conf.Daemon.WebPort,
		}

		err = websrv.ListenAndServe()
		if err != nil {
			log.Errorf("ListenAndServe: websrv %s", err.Error())
		}
	}()

	err = xhttp.ListenAndServe(
		&http.Server{
			Handler:      rgatesrv,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, S3ModeDevel || isLite(), func(s string) { log.Debugf(s) })
	if err != nil {
		log.Errorf("ListenAndServe: gatesrv %s", err.Error())
	}

	radosFini()
	dbDisconnect()
}
