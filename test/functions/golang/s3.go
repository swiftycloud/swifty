package main

import (
	"fmt"
	"strings"
	"io/ioutil"
	"swifty"
	"encoding/json"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/aws/awserr"
)

func showS3Err(err error) {
	if aerr, ok := err.(awserr.Error); ok {
		switch aerr.Code() {
		case s3.ErrCodeNoSuchBucket:
			fmt.Println(s3.ErrCodeNoSuchBucket, aerr.Error())
		default:
			fmt.Println(aerr.Error())
		}
	} else {
		fmt.Println(err.Error())
	}
}

func Main(args map[string]string) interface{} {
	svc, err := swifty.S3BucketProt("images", "http")
	if err != nil {
		panic("Can't get bkt")
	}

	var claims map[string]interface{}

	err = json.Unmarshal([]byte(args["_SWY_JWT_CLAIMS_"]), &claims)
	if err != nil {
		fmt.Println(err)
		panic("Can't unmarshal claims")
	}

	if args["action"] == "put" {
		input := &s3.PutObjectInput{
			Bucket:	aws.String("images"),
			Key:	aws.String(claims["cookie"].(string)),
			Body:	aws.ReadSeekCloser(strings.NewReader(args["_SWY_BODY_"])),
		}

		_, err := svc.PutObject(input)
		if err != nil {
			showS3Err(err)
			panic("Can't put object")
		}

		return "OK"
	}

	if args["action"] == "get" {
		input := &s3.GetObjectInput{
			Bucket: aws.String("images"),
			Key:    aws.String(claims["cookie"].(string)),
		}

		result, err := svc.GetObject(input)
		if err != nil {
			showS3Err(err)
			panic("Can't put object")
		}

		v, err := ioutil.ReadAll(result.Body)
		if err != nil {
			panic(fmt.Errorf("Can't read body: %s", err.Error()))
		}

		return map[string]interface{} { "img": string(v) }
	}

	if args["action"] == "del" {
		input := &s3.DeleteObjectInput{
			Bucket: aws.String("images"),
			Key:    aws.String(claims["cookie"].(string)),
		}

		_, err := svc.DeleteObject(input)
		if err != nil {
			showS3Err(err)
			panic("Can't put object")
		}

		return "OK"
	}

	return "Bad request"
}
