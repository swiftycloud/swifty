package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"gopkg.in/mgo.v2"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
	"flag"
	"fmt"

	"../common"
)

var s3Secrets map[string]string

type YAMLConfCeph struct {
	ConfigPath	string			`yaml:"config-path"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	LogLevel	string			`yaml:"loglevel"`
}

type YAMLConfDB struct {
	Name		string			`yaml:"state"`
	Addr		string			`yaml:"address"`
	User		string			`yaml:"user"`
	Pass		string			`yaml:"password"`
}

type YAMLConf struct {
	DB		YAMLConfDB		`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Ceph		YAMLConfCeph		`yaml:"ceph"`
}

var conf YAMLConf
var log *zap.SugaredLogger
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

	swy.InitLogger(log)
}

func formatRequest(prefix string, r *http.Request) string {
//	var request []string
//
//	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
//	request = append(request, prefix)
//	request = append(request, url)
//	request = append(request, fmt.Sprintf("Host: %v", r.Host))
//
//	for name, headers := range r.Header {
//		for _, h := range headers {
//			request = append(request, fmt.Sprintf("%v:%v", name, h))
//		}
//	}
//	return strings.Join(request, "\n")
	return prefix
}

func handleBucket(w http.ResponseWriter, r *http.Request) {
	var bucket_name string = mux.Vars(r)["BucketName"]
	var akey *S3AccessKey
	var bucket *S3Bucket
	var err error

	log.Debug(formatRequest(fmt.Sprintf("handleBucket: bucket %v",
						bucket_name), r))

	akey, err = s3VerifyAuthorization(r)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPut:
	case http.MethodGet:
	case http.MethodDelete:
	case http.MethodHead:
		break
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	// Setup default ACL
	if verifyAclValue(r.Header.Get("x-amz-acl"), BucketAcls) == false {
		r.Header.Set("x-amz-acl", S3BucketAclPrivate)
	}

	bucket = &S3Bucket{
		Name:			bucket_name,
		Acl:			r.Header.Get("x-amz-acl"),
	}

	if bucket_name == "" {
		if r.Method == http.MethodGet {
			bucketFound, err := bucket.dbFindByKey(akey)
			if err != nil {
				http.Error(w, "Can't find buckets", http.StatusBadRequest)
				return
			}
			bucket.Name = bucketFound.Name
		} else {
			http.Error(w, "Empty bucket name provided", http.StatusBadRequest)
			return
		}
	}

	switch r.Method {
	case http.MethodPut:
		// Create a new bucket
		err = s3InsertBucket(akey, bucket)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodGet:
		// List all objects
		objects, err := s3ListBucket(akey, bucket)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err = HTTPMarshalXMLAndWriteOK(w, objects)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		return
		break
	case http.MethodDelete:
		// Delete a bucket
		err = s3DeleteBucket(akey, bucket)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodHead:
		// Check if we can access a bucket
		err = s3CheckAccess(akey, bucket.Name, "")
		if err != nil {
			if err == mgo.ErrNotFound {
				http.Error(w, "No bucket found", http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		break
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	w.WriteHeader(http.StatusOK)
}

func handleObject(w http.ResponseWriter, r *http.Request) {
	var bucket_name string = mux.Vars(r)["BucketName"]
	var object_name string = mux.Vars(r)["ObjName"]
	var object_size int64
	var akey *S3AccessKey
	var bucket *S3Bucket
	var object *S3Object
	var body []byte
	var err error

	defer r.Body.Close()

	log.Debug(formatRequest(fmt.Sprintf("handleObject: bucket %v object %v",
						bucket_name, object_name), r))

	akey, err = s3VerifyAuthorization(r)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
	case http.MethodPut:
	case http.MethodGet:
	case http.MethodDelete:
	case http.MethodHead:
		break
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	if bucket_name == "" {
		http.Error(w, "Empty bucket name provided", http.StatusBadRequest)
		return
	} else if object_name == "" {
		http.Error(w, "Empty object name provided", http.StatusBadRequest)
		return
	}

	// Setup default ACL
	if verifyAclValue(r.Header.Get("x-amz-acl"), ObjectAcls) == false {
		r.Header.Set("x-amz-acl", S3ObjectAclPrivate)
	}

	bucket = &S3Bucket{
		Name:			bucket_name,
	}

	object_size, err = strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		object_size = 0
	}

	object = &S3Object{
		Name:			object_name,
		Acl:			r.Header.Get("x-amz-acl"),
		Version:		1,
		Size:			object_size,
	}

	switch r.Method {
	case http.MethodPost:
		// Create new object in bucket
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	case http.MethodPut:
		body, err = ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Can't read data: %s", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Create new object
		bucket, object, err = s3InsertObject(akey, bucket, object)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err = s3CommitObject(bucket, object, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodGet:
		// List all objects
		body, err = s3ReadObject(akey, bucket, object)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
		break
	case http.MethodDelete:
		// Delete a bucket
		err = s3DeleteObject(akey, bucket, object)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodHead:
		// Check if we can access a bucket
		err = s3CheckAccess(akey, bucket_name, object_name)
		if err != nil {
			if err == mgo.ErrNotFound {
				http.Error(w, "No bucket/object found", http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		break
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	w.WriteHeader(http.StatusOK)
}

func handleAdmin(w http.ResponseWriter, r *http.Request) {
	if s3VerifyAdmin(r) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	var secretsDisabled bool
	var dbPass string
	var config_path string
	var err error

	flag.StringVar(&config_path,
			"conf",
				"",
				"path to a config file")
	flag.BoolVar(&radosDisabled,
			"no-rados",
				false,
				"disable rados")
	flag.BoolVar(&secretsDisabled,
			"no-secrets",
				false,
				"disable secrets engine")
	flag.Int64Var(&cachedObjSize,
			"cached-obj-size",
				S3StorageSizePerObj,
				"object size in bytes to put into cache")
	flag.StringVar(&dbPass,
			"db-pass",
				"",
				"database password")
	flag.Parse()

	if config_path != "" {
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	log.Debugf("config: %v", &conf)

	if cachedObjSize > S3StorageSizePerObj {
		log.Errorf("Caching more than %d bytes is not allowed",
				S3StorageSizePerObj)
		return
	}

	if secretsDisabled {
		if dbPass == "" {
			log.Errorf("Provide db pass")
			return
		}
		s3Secrets = map[string]string {
			conf.DB.Pass: dbPass,
		}
	} else {
		s3Secrets, err = swy.ReadSecrets("s3")
		if err != nil {
			log.Errorf("Can't read gate secrets")
			return
		}
	}

	r := mux.NewRouter()

	match_bucket := "/{BucketName:[a-zA-Z0-9-.]*}"
	match_object := "/{BucketName:[a-zA-Z0-9-.]+}/{ObjName:[a-zA-Z0-9-.]+}"

	// Servise operations
	r.HandleFunc(match_bucket,	handleBucket)
	r.HandleFunc(match_object,	handleObject)

	// Admin operations
	r.HandleFunc("/v1/api/admin",	handleAdmin)

	err = dbConnect(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to backend: %s",
				err.Error())
	}

	err = radosInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to rados: %s",
				err.Error())
	}

	gatesrv = &http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
	}

	err = gatesrv.ListenAndServe()
	if err != nil {
		log.Errorf("ListenAndServe: %s", err.Error())
	}

	radosFini()
	dbDisconnect()
}
