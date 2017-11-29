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

access_key = '6DLA43X797XL2I42IJ33'
secret_key = 'AJwz9vZpdnz6T5TqEDQOEFos6wxxCnW0qwLQeDcB'
endpoint_url = 'http://localhost:8787/'

print("Connecting to endpoint %s with keys %s / %s" %
      (endpoint_url, access_key, secret_key))

s3 = boto3.session.Session().client(service_name = 's3',
                                    aws_access_key_id = access_key,
                                    aws_secret_access_key = secret_key,
                                    endpoint_url = endpoint_url)

bucket_name = genBucketName()
s3.create_bucket(Bucket = bucket_name)

object_name = genObjectName()
object_body = genRandomData(32)
s3.put_object(Bucket = bucket_name, Key = object_name, Body = object_body)

response = s3.get_object(Bucket = bucket_name, Key = object_name)
if response['ContentLength'] > 0:
    body = response['Body'].read().decode("utf-8")
    if body == object_body:
        print("PASS: %s" % (object_body))
    else:
        print("FAIL: expected %s but got %s" % (object_body, body))

s3.delete_object(Bucket = bucket_name, Key = object_name)
s3.delete_bucket(Bucket = bucket_name)
