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

func Main(rq *Request) (interface{}, *Responce) {
	svc, err := swifty.S3BucketProt("images", "http")
	if err != nil {
		panic("Can't get bkt")
	}

	if rq.Args["action"] == "put" {
		input := &s3.PutObjectInput{
			Bucket:	aws.String("images"),
			Key:	aws.String(rq.Claims["cookie"].(string)),
			Body:	aws.ReadSeekCloser(strings.NewReader(rq.Body)),
		}

		_, err := svc.PutObject(input)
		if err != nil {
			showS3Err(err)
			panic("Can't put object")
		}

		return "OK"
	}

	if rq.Args["action"] == "get" {
		input := &s3.GetObjectInput{
			Bucket: aws.String("images"),
			Key:    aws.String(rq.Claims["cookie"].(string)),
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

	if rq.Args["action"] == "del" {
		input := &s3.DeleteObjectInput{
			Bucket: aws.String("images"),
			Key:    aws.String(rq.Claims["cookie"].(string)),
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
