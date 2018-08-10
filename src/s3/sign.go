package main

import (
	"crypto/sha256"
	"encoding/hex"
	"crypto/hmac"
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"regexp"
	"sort"
	"bytes"
	"fmt"
	"./mgo"
)

const (
	AWSAuthHeaderPrefix	= "AWS4-HMAC-SHA256"
	AWSEmptyStringSHA256	= "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	AWSUnsignedPayload	= "UNSIGNED-PAYLOAD"
	AWS4ServiceS3		= "s3"
	AWS4ServiceCW		= "monitoring"
	AWS4Request		= "aws4_request"
)

type AuthContext struct {
	// From "Authorization" header
	AccessKey		string
	ShortTimeStamp		string
	Region			string
	Service			string
	SignedHeaders		[]string
	Signature		string

	// Building from headers
	ContentSha256		string
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

func (actx *AuthContext) ParseAuthorization(authHeader string) error {
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
			actx.AccessKey		= creds[0]
			actx.ShortTimeStamp	= creds[1]
			actx.Region		= creds[2]
			actx.Service		= creds[3]
			if (creds[3] != AWS4ServiceS3 &&
				creds[3] != AWS4ServiceCW) ||
				creds[4] != AWS4Request {
				return fmt.Errorf("s3: Wrong request type %s in header", e)
			}
			break
		case "SignedHeaders":
			actx.SignedHeaders = strings.Split(e[pos+1:], ";")
			if len(actx.SignedHeaders) < 1 {
				return fmt.Errorf("s3: Wrong signed header %s in header", e)
			}
			sort.Strings(actx.SignedHeaders)
			break;
		case "Signature":
			actx.Signature = e[pos+1:]
			break
		}
	}

	// Verify fields
	if !hasAWSAuthHeaderPrefix {
		return fmt.Errorf("s3: No %s prefix detected", AWSAuthHeaderPrefix)
	} else if actx.Signature == "" {
		return fmt.Errorf("s3: Empty signature decected")
	} else if len(actx.SignedHeaders) < 1 {
		return fmt.Errorf("s3: Empty signed headers decected")
	} else if actx.AccessKey == "" || actx.ShortTimeStamp == "" ||
		actx.Region == "" {
		return fmt.Errorf("s3: Empty credentials decected")
	}

	return nil
}

func (actx *AuthContext) BuildBodyDigest(r *http.Request) (error) {
	if actx.ContentSha256 == AWSUnsignedPayload {
		actx.BodyDigest = AWSUnsignedPayload
	} else if r.Body == nil {
		actx.BodyDigest = AWSEmptyStringSHA256
	} else {
		buf, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return err
		}
		r.Body = ioutil.NopCloser(bytes.NewBuffer(buf))
		actx.BodyDigest = hex.EncodeToString(makeSha256(buf))
	}
	return nil
}

func queryEncode(uri string) string {
	uri = strings.Replace(url.QueryEscape(uri), "+", "%20", -1)
	return strings.TrimSpace(uri)
}

func genCanonicalHeader(r *http.Request, key string) string {
	name := strings.TrimSpace(strings.ToLower(key))
	value := strings.TrimSpace(r.Header.Get(http.CanonicalHeaderKey(key)))
	if value == "" || name == "" {
		return ""
	}
	return name + ":" + value
}

func getHeader(r *http.Request, name string) string {
	return strings.TrimSpace(r.Header.Get(name))
}

func (actx *AuthContext) BuildCanonicalString(r *http.Request) {
	var members []string
	var keys []string

	// HTTPMethod
	members = append(members, r.Method)

	// CanonicalURI
	members = append(members, r.URL.Path)

	// CanonicalQueryString
	q := r.URL.Query()
	for k, _ := range q {
		keys = append(keys, k)
	}

	if len(keys) > 0 {
		var queries []string
		var query string

		sort.Strings(keys)

		for _, k := range keys {
			query = queryEncode(k) + "="
			if len(q[k]) > 0 {
				query += queryEncode(q[k][0])
			}
			queries = append(queries, query)
		}
		members = append(members, strings.Join(queries, "&"))
	} else {
		members = append(members, "")
	}

	// Canonical headers
	for _, k := range actx.SignedHeaders {
		if k == "host" {
			members = append(members, "host:" + strings.TrimSpace(r.Host))
			continue
		}
		members = append(members, genCanonicalHeader(r, k))
	}
	members = append(members, "")

	// SignedHeaders
	members = append(members, strings.Join(actx.SignedHeaders, ";"))

	// HashedPayload
	members = append(members, actx.BodyDigest)

	actx.CanonicalString = strings.Join(members, "\n")
}

func (actx *AuthContext) BuildSigningKey(secret string) {
	a := makeHmac([]byte("AWS4" + secret),
			[]byte(actx.ShortTimeStamp))
	b := makeHmac(a, []byte(actx.Region))
	c := makeHmac(b, []byte(actx.Service))
	actx.SigningKey = makeHmac(c, []byte(AWS4Request))
}

func (actx *AuthContext) BuildStringToSign() {
	actx.StringToSign = strings.Join([]string{
		AWSAuthHeaderPrefix,
		actx.LongTimeStamp,
		strings.Join([]string{
			actx.ShortTimeStamp,
			actx.Region,
			actx.Service,
			AWS4Request,
		}, "/"),
		hex.EncodeToString(makeSha256([]byte(actx.CanonicalString))),
	}, "\n")
}

func (actx *AuthContext) BuildSignature() {
	signature := makeHmac(actx.SigningKey, []byte(actx.StringToSign))
	actx.BuiltSignature = hex.EncodeToString(signature)
}

func s3VerifyAuthorizationHeaders(ctx context.Context, r *http.Request, authHeader string) (*s3mgo.S3AccessKey, error) {
	var akey *s3mgo.S3AccessKey
	var actx AuthContext
	var err error

	err = actx.ParseAuthorization(authHeader)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	akey, err = LookupAccessKey(ctx, actx.AccessKey)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	actx.BuildSigningKey(s3DecryptAccessKeySecret(akey))
	actx.LongTimeStamp = getHeader(r, "X-Amz-Date")
	actx.ContentSha256 = getHeader(r, "X-Amz-Content-Sha256")

	err = actx.BuildBodyDigest(r)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	actx.BuildCanonicalString(r)
	actx.BuildStringToSign()
	actx.BuildSignature()

	log.Debugf("s3: s3VerifyAuthorizationHeaders: %s %s",
		actx.Signature, actx.BuiltSignature)
	if actx.Signature == actx.BuiltSignature {
		return akey, nil
	}

	return nil, fmt.Errorf("Signature mismatch")
}

func s3AuthorizeUser(ctx context.Context, r *http.Request) (*s3mgo.S3AccessKey, error) {
	var authHeader string

	authHeader = getHeader(r, "Authorization")
	if authHeader == "" {
		return nil, nil
	}

	return s3VerifyAuthorizationHeaders(ctx, r, authHeader)
}
