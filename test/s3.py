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

bucket_name = 'bucket-1'
s3 = boto3.session.Session().client(service_name = 's3',
                                    aws_access_key_id = access_key,
                                    aws_secret_access_key = secret_key,
                                    endpoint_url = endpoint_url)

s3.create_bucket(Bucket = bucket_name)
#s3.delete_bucket(Bucket = bucket_name)
#response = s3.list_buckets(Bucket = bucket_name)
#print(response)

with open(os.getcwd() + '/test/s3.py', 'rb') as data:
    s3.put_object(Bucket = bucket_name, Key = 's3-1.py', Body = data)
    response = s3.get_object(Bucket = bucket_name, Key = 's3-1.py')
    print(response)
    if response['ContentLength'] > 0:
        body = response['Body']
        print(body.read())
    #respose = s3.delete_object(Bucket = bucket_name, Key = 's3-1.py')
    #print(response)

#with open('test/s3.py', 'rb') as data:
#    s3.put_object(Bucket = bucket_name, Key = 's3-2.py', Body = data)

#s3.delete_object(Bucket = bucket_name, Key = key1)
#s3.delete_object(Bucket = bucket_name, Key = key2)

#response = s3.list_objects(Bucket = bucket_name)
#print(response)
