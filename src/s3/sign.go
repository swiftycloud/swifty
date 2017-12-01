package main

import (
	"crypto/sha256"
	"encoding/hex"
	"crypto/hmac"
	"io/ioutil"
	"net/http"
	"strings"
	"regexp"
	"sort"
	"bytes"
	"fmt"
)

const (
	AWSAuthHeaderPrefix	= "AWS4-HMAC-SHA256"
	AWSEmptyStringSHA256	= "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	AWS4Service		= "s3"
	AWS4Request		= "aws4_request"
)

type AuthContext struct {
	// From "Authorization" header
	AccessKey		string
	ShortTimeStamp		string
	Region			string
	SignedHeaders		[]string
	Signature		string

	// Building from headers
	LongTimeStamp		string
	BodyDigest		string
	CanonicalString		string
	SigningKey		[]byte
	StringToSign		string
	BuiltSignature		string
}

func makeHmac(key []byte, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	hash.Write(data)
	return hash.Sum(nil)
}

func makeSha256(data []byte) []byte {
	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil)
}

func (ctx *AuthContext) ParseAuthorization(authHeader string) error {
	var hasAWSAuthHeaderPrefix bool
	var elems []string
	var e string

	if authHeader == "" ||
		len(authHeader) < len(AWSAuthHeaderPrefix) ||
		authHeader[:len(AWSAuthHeaderPrefix)] != AWSAuthHeaderPrefix {
		return fmt.Errorf("s3: No 'Authorization' header provided")
	}

	re := regexp.MustCompile(" ")
	authHeader = strings.Join(strings.Fields(strings.Replace(authHeader, ",", " ", -1)), " ")
	elems = re.Split(authHeader, -1)

	if len(elems) < 3 {
		return fmt.Errorf("s3: Not enough fields in 'Authorization' header")
	}

	for _, e = range elems {
		pos := strings.Index(e, "=")
		if pos < 0 {
			if e == AWSAuthHeaderPrefix {
				hasAWSAuthHeaderPrefix = true
			} else {
				return fmt.Errorf("s3: Unknown value '%s' in header", e)
			}
			continue
		}
		switch e[:pos] {
		case "Credential":
			creds := strings.Split(e[pos+1:], "/")
			if len(creds) < 5 {
				return fmt.Errorf("s3: Wrong credential %s in header", e)
			}
			ctx.AccessKey		= creds[0]
			ctx.ShortTimeStamp	= creds[1]
			ctx.Region		= creds[2]
			if creds[3] != AWS4Service ||
				creds[4] != AWS4Request {
				return fmt.Errorf("s3: Wrong request type %s in header", e)
			}
			break
		case "SignedHeaders":
			ctx.SignedHeaders = strings.Split(e[pos+1:], ";")
			if len(ctx.SignedHeaders) < 1 {
				return fmt.Errorf("s3: Wrong signed header %s in header", e)
			}
			sort.Strings(ctx.SignedHeaders)
			break;
		case "Signature":
			ctx.Signature = e[pos+1:]
			break
		}
	}

	// Verify fields
	if !hasAWSAuthHeaderPrefix {
		return fmt.Errorf("s3: No %s prefix detected", AWSAuthHeaderPrefix)
	} else if ctx.Signature == "" {
		return fmt.Errorf("s3: Empty signature decected")
	} else if len(ctx.SignedHeaders) < 1 {
		return fmt.Errorf("s3: Empty signed headers decected")
	} else if ctx.AccessKey == "" || ctx.ShortTimeStamp == "" ||
		ctx.Region == "" {
		return fmt.Errorf("s3: Empty credentials decected")
	}

	return nil
}

func (ctx *AuthContext) BuildBodyDigest(r *http.Request) (error) {
	if r.Body == nil {
		ctx.BodyDigest = AWSEmptyStringSHA256
	} else {
		buf, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return err
		}
		r.Body = ioutil.NopCloser(bytes.NewBuffer(buf))
		ctx.BodyDigest = hex.EncodeToString(makeSha256(buf))
	}
	return nil
}

func removeDotSegments(url string) string {
	if url != "" {
		re := regexp.MustCompile("/+")
		url = strings.Replace(url, "..", "", -1)
		url = string((re.ReplaceAll([]byte(url), []byte("/")))[:])
	}
	return url
}

func uriEncode(uri string) string {
	uri = strings.Replace(uri, "+", "%20", -1)
	return strings.TrimSpace(uri)
}

func genCanonicalHeader(r *http.Request, key string) string {
	name := uriEncode(strings.ToLower(key))
	value := uriEncode(r.Header.Get(http.CanonicalHeaderKey(key)))
	if value == "" || name == "" {
		return ""
	}
	return name + ":" + value
}

func getHeader(r *http.Request, name string) string {
	return strings.TrimSpace(r.Header.Get(name))
}

func getCanonicalHeader(r *http.Request, name string) string {
	hname := uriEncode(name)
	value := strings.TrimSpace(r.Header.Get(name))
	if value == "" || hname == "" {
		return ""
	}
	return hname + ":" + value
}

func (ctx *AuthContext) BuildCanonicalString(r *http.Request) {
	var members []string
	var keys []string

	// HTTPMethod
	members = append(members, r.Method)

	// CanonicalURI
	members = append(members, removeDotSegments(r.URL.Path))

	// CanonicalQueryString
	q := r.URL.Query()
	for k, _ := range q {
		keys = append(keys, k)
	}

	if len(keys) > 0 {
		var query string = ""
		sort.Strings(keys)
		for i, k := range keys {
			query += uriEncode(k) + "="
			if len(q[k]) > 0 {
				query += uriEncode(q[k][0])
			}
			if i < len(keys) {
				query += "&"
			}
		}
		members = append(members, query)
	} else {
		members = append(members, "")
	}

	// Canonical headers
	for _, k := range ctx.SignedHeaders {
		if k == "host" {
			members = append(members, "host:" + uriEncode(r.Host))
			continue
		}
		members = append(members, genCanonicalHeader(r, k))
	}
	members = append(members, "")

	// SignedHeaders
	members = append(members, strings.Join(ctx.SignedHeaders, ";"))

	// HashedPayload
	members = append(members, ctx.BodyDigest)

	ctx.CanonicalString = strings.Join(members, "\n")
}

func (ctx *AuthContext) BuildSigningKey(secret string) {
	a := makeHmac([]byte("AWS4" + secret),
			[]byte(ctx.ShortTimeStamp))
	b := makeHmac(a, []byte(ctx.Region))
	c := makeHmac(b, []byte(AWS4Service))
	ctx.SigningKey = makeHmac(c, []byte(AWS4Request))
}

func (ctx *AuthContext) BuildStringToSign() {
	ctx.StringToSign = strings.Join([]string{
		AWSAuthHeaderPrefix,
		ctx.LongTimeStamp,
		strings.Join([]string{
			ctx.ShortTimeStamp,
			ctx.Region,
			AWS4Service,
			AWS4Request,
		}, "/"),
		hex.EncodeToString(makeSha256([]byte(ctx.CanonicalString))),
	}, "\n")
}

func (ctx *AuthContext) BuildSignature() {
	signature := makeHmac(ctx.SigningKey, []byte(ctx.StringToSign))
	ctx.BuiltSignature = hex.EncodeToString(signature)
}

func s3VerifyAuthorization(r *http.Request) (*S3AccessKey, error) {
	var akey *S3AccessKey
	var ctx AuthContext
	var err error

	err = ctx.ParseAuthorization(getHeader(r, "Authorization"))
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	akey, err = dbLookupAccessKey(ctx.AccessKey)
	if err != nil {
		return nil, err
	}

	ctx.BuildSigningKey(akey.AccessKeySecret)
	ctx.LongTimeStamp = getHeader(r, "X-Amz-Date")

	err = ctx.BuildBodyDigest(r)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	ctx.BuildCanonicalString(r)
	ctx.BuildStringToSign()
	ctx.BuildSignature()

	log.Debugf("s3: verifyRequestSignature: %s %s",
		ctx.Signature, ctx.BuiltSignature)
	if ctx.Signature == ctx.BuiltSignature {
		return akey, nil
	}

	return nil, fmt.Errorf("Signature mismatch")
}
