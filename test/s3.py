import argparse
import boto3
import random
import string
import os

def genRandomData(len):
    return ''.join(random.SystemRandom().choice(string.ascii_uppercase + string.digits) for _ in range(len))

def genBucketName():
    return genRandomData(6)

def genObjectName():
    return genRandomData(10)

parser = argparse.ArgumentParser()
parser.add_argument('--access-key', dest = 'access_key',
                    default = '6DLA43X797XL2I42IJ33',
                    help = 'access key')
parser.add_argument('--secret-key', dest = 'secret_key',
                    default = 'AJwz9vZpdnz6T5TqEDQOEFos6wxxCnW0qwLQeDcB',
                    help = 'secret key')
parser.add_argument('--endpoint-url', dest = 'endpoint_url',
                    default = 'http://192.168.122.197:8787/',
                    help = 'S3 service address')
parser.add_argument('--bucket-name', dest = 'bucket_name',
                    default = genBucketName(),
                    help = 'bucket name to use')
parser.add_argument('--object-name', dest = 'object_name',
                    default = genObjectName(),
                    help = 'object name to use')
parser.add_argument('--object-body', dest = 'object_body',
                    default = genRandomData(32),
                    help = 'object body to use')
args = parser.parse_args()

print("Connecting to endpoint %s with keys %s / %s" %
      (args.endpoint_url, args.access_key, args.secret_key))

s3 = boto3.session.Session().client(service_name = 's3',
                                    aws_access_key_id = args.access_key,
                                    aws_secret_access_key = args.secret_key,
                                    endpoint_url = args.endpoint_url)

print("Creating bucket %s with object %s" % (args.bucket_name, args.object_name))
s3.create_bucket(Bucket = args.bucket_name)

s3.put_object(Bucket = args.bucket_name, Key = args.object_name, Body = args.object_body)

response = s3.get_object(Bucket = args.bucket_name, Key = args.object_name)
if response['ContentLength'] > 0:
    body = response['Body'].read().decode("utf-8")
    if body == args.object_body:
        print("PASS: %s" % (args.object_body))
    else:
        print("FAIL: expected %s but got %s" % (args.object_body, body))

#s3.delete_object(Bucket = args.bucket_name, Key = args.object_name)
#s3.delete_bucket(Bucket = args.bucket_name)
