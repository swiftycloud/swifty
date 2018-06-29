
//	if args["action"] == "list" {
//		input := &s3.ListObjectsV2Input{
//			Bucket:  aws.String("images"),
//		}
//
//		result, err := svc.ListObjectsV2(input)
//		if err != nil {
//			showS3Err(err)
//			panic("Can't list objects")
//		}
//
//		onames := []string{}
//		for _, obj := range result.Contents {
//			onames = append(onames, *obj.Key)
//		}
//
//		return onames
//	}

