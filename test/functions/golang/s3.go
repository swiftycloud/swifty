package main

import (
	"os"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
)

func s3session() *s3.S3 {
	addr := os.Getenv("MWARE_S3IMAGES_ADDR")
	akey := os.Getenv("MWARE_S3IMAGES_KEY")
	asec := os.Getenv("MWARE_S3IMAGES_SECRET")

	if addr == "" || akey == "" || asec == "" {
		panic("No bucket attached")
	}

	ses := session.Must(session.NewSession())

	return s3.New(ses, &aws.Config{
		Region: aws.String("internal"),
		Credentials: credentials.NewStaticCredentials(akey, asec, ""),
		Endpoint: aws.String(addr),
	})
}

func main() {
	svc := s3session()

	result, err := svc.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			fmt.Println(aerr.Error())
		} else {
			fmt.Println(err.Error())
		}
		panic("Can't list buckets")
	}

	fmt.Println(result)
}
