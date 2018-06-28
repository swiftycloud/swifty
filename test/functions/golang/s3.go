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

	ses := session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String("internal"),
			Credentials: credentials.NewStaticCredentials(akey, asec, ""),
			Endpoint: aws.String("http://" + addr),
			S3ForcePathStyle: aws.Bool(true),
			S3UseAccelerate: aws.Bool(false),
		},
	}))

	return s3.New(ses)
}

func Main(args map[string]string) interface{} {
	svc := s3session()

	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String("images"),
	}

	result, err := svc.ListObjectsV2(input)
	if err != nil {
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
		panic("Can't list objects")
	}

	fmt.Printf("Answered:\n")
	onames := []string{}
	for _, obj := range result.Contents {
		onames = append(onames, *obj.Key)
	}

	return onames
}

func main() {
	Main(nil)
}
