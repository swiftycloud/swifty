#!/usr/bin/python3

import http.client
import argparse
import random
import string
import urllib
import boto3
import json
import sys
import os

def genRandomData(len):
    return ''.join(random.SystemRandom().choice(string.ascii_uppercase + string.digits) for _ in range(len))

def genBucketName():
    return genRandomData(6)

def genObjectName():
    return genRandomData(10)

def json_get(key, data):
    if key in data:
        return data[key]
    return None

parser = argparse.ArgumentParser(prog='s3ctl.py')
parser.add_argument('--admin-secret', dest = 'admin_secret',
                    help = 'access token to ented admin interface')
parser.add_argument('--endpoint-url', dest = 'endpoint_url',
                    default = '192.168.122.197:8787',
                    help = 'S3 service address')
parser.add_argument('--access-key-id', dest = 'access_key_id',
                    help = 'Access key')
parser.add_argument('--secret-key-id', dest = 'secret_key_id',
                    help = 'Secret key')

sp = parser.add_subparsers(dest = 'cmd')
for cmd in ['keygen']:
    spp = sp.add_parser(cmd, help = 'Generate keys')
    spp.add_argument('--namespace', dest = 'namespace',
                     help = 'Unique namespace', required = True)
    spp.add_argument('--lifetime', dest = 'lifetime', type = int,
                     help = 'Key lifetime', default = 0, required = False)
    spp.add_argument('--name', dest = 'name',
                     help = 'Bucket name', required = False)
    spp.add_argument('--save', dest = 'save',
                     help = 'Save creds in file', action = 'store_true')

for cmd in ['keydel']:
    spp = sp.add_parser(cmd, help = 'Delete keys')

for cmd in ['list-buckets']:
    spp = sp.add_parser(cmd, help = 'List buckets')

for cmd in ['list-objects']:
    spp = sp.add_parser(cmd, help = 'List objects in bucket')
    spp.add_argument('--name', dest = 'name',
                     help = 'Bucket name', required = True)

for cmd in ['list-uploads']:
    spp = sp.add_parser(cmd, help = 'List object parts being uploaded')
    spp.add_argument('--name', dest = 'name',
                     help = 'Bucket name', required = True)

for cmd in ['bucket-add']:
    spp = sp.add_parser(cmd, help = 'Create bucket')
    spp.add_argument('--name', dest = 'name',
                     help = 'Bucket name', required = True)

for cmd in ['bucket-del']:
    spp = sp.add_parser(cmd, help = 'Delete bucket')
    spp.add_argument('--name', dest = 'name',
                     help = 'Bucket name', required = True)

for cmd in ['folder-add']:
    spp = sp.add_parser(cmd, help = 'Create directory')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Directory name')

for cmd in ['object-add']:
    spp = sp.add_parser(cmd, help = 'Create object')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name')
    spp.add_argument('--file', dest = 'file', help = 'Content from file')

for cmd in ['object-copy']:
    spp = sp.add_parser(cmd, help = 'Copy object')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name', required = True)
    spp.add_argument('--dst-name', dest = 'dst_name', required = True)
    spp.add_argument('--dst-key', dest = 'dst_key', required = True)

for cmd in ['object-del']:
    spp = sp.add_parser(cmd, help = 'Delete object')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name', required = True)

for cmd in ['object-part-init']:
    spp = sp.add_parser(cmd, help = 'Initialize multipart object upload')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name')

for cmd in ['object-part-fini']:
    spp = sp.add_parser(cmd, help = 'Finalize multipart object upload')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name', required = True)
    spp.add_argument('--id', dest = 'id', help = 'Upload id', required = True)
    spp.add_argument('--list', dest = 'list', help = 'part:etag,[...] list', required = True)

for cmd in ['object-part-abort']:
    spp = sp.add_parser(cmd, help = 'Abort multipart object upload')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name', required = True)
    spp.add_argument('--id', dest = 'id', help = 'Upload id', required = True)

for cmd in ['object-part-add']:
    spp = sp.add_parser(cmd, help = 'Upload part of multipart object')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name', required = True)
    spp.add_argument('--part', dest = 'part', help = 'Part number', required = True)
    spp.add_argument('--id', dest = 'id', help = 'Upload id', required = True)
    spp.add_argument('--file', dest = 'file', help = 'File to upload')

for cmd in ['object-part-list']:
    spp = sp.add_parser(cmd, help = 'List object parts')
    spp.add_argument('--name', dest = 'name', help = 'Bucket name', required = True)
    spp.add_argument('--key', dest = 'key', help = 'Object name', required = True)
    spp.add_argument('--id', dest = 'id', help = 'Upload id', required = True)

for cmd in ['notify']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--namespace', dest = 'Namespace', required = True)
    spp.add_argument('--bucket', dest = 'Bucket name', required = True)
    spp.add_argument('--queue', dest = 'Queue', required = True)

args = parser.parse_args()

if args.cmd == None:
    parser.print_help()
    sys.exit(1)

conf_path = "~/.swysecrets/s3ctl.json"

def saveCreds(args):
    try:
        with open(os.path.expanduser(conf_path), "w") as f:
            creds = {
                'access-key-id': args.access_key_id,
                'access-key-secret': args.secret_key_id,
                'admin-secret': args.admin_secret,
            }
            f.write(json.dumps(creds))
            f.close()
            os.chmod(os.path.expanduser(conf_path), 0o600)
    except:
        return None

def readCreds():
    try:
        with open(os.path.expanduser(conf_path), "r") as f:
            creds = json.loads(f.read())
            f.close()
            return creds
    except:
        return None

creds = readCreds()
if creds != None:
    if not args.access_key_id:
        args.access_key_id = json_get('access-key-id', creds)
    if not args.secret_key_id:
        args.secret_key_id = json_get('access-key-secret', creds)
    if not args.admin_secret:
        args.admin_secret = json_get('admin-secret', creds)

def resp_error(cmd, resp):
    if resp != None:
        print("ERROR: Command '%s' failed %d with: %s" % \
            (cmd, resp.status, resp.read().decode('utf-8')))
    else:
        print("ERROR: Command '%s' failed" % (cmd))
    sys.exit(1)

def request_admin(cmd, data):
    params = urllib.parse.urlencode({'cmd': cmd})
    headers = {"X-SwyS3-Token": args.admin_secret,
               'Content-type': 'application/json'}
    try:
        conn = http.client.HTTPConnection(args.endpoint_url)
        conn.request('POST','/v1/api/admin/' + args.cmd, json.dumps(data), headers)
        return conn.getresponse()
    except ConnectionError as e:
        print("ERROR: Can't process request (%s / %s): %s" % \
              (cmd, repr(data), repr(e)))
        return None
    except:
        return None

def request_notify(data):
    headers = {"X-SwyS3-Token": args.admin_secret,
               'Content-type': 'application/json'}
    try:
        conn = http.client.HTTPConnection(args.endpoint_url)
        conn.request('POST','/v1/api/notify/subscribe', json.dumps(data), headers)
        return conn.getresponse()
    except ConnectionError as e:
        print("ERROR: Can't set notify (%s): %s" % \
                (repr(data), repr(e)))
        return None
    except:
        return None

def make_s3(endpoint_url, access_key, secret_key):
    endpoint_url = "http://" + endpoint_url + "/"
    print("Connecting to endpoint %s with keys %s / %s" %
          (endpoint_url, access_key, secret_key))
    try:
        return boto3.session.Session().client(service_name = 's3',
                                              aws_access_key_id = access_key,
                                              aws_secret_access_key = secret_key,
                                              endpoint_url = endpoint_url)
    except ConnectionError as e:
        print("ERROR: Can't connect: %s" % (endpoint_url, repr(e)))
        return None
    except:
        return None

if args.cmd not in ['keygen', 'keydel', 'keygetroot', 'notify']:
    s3 = make_s3(args.endpoint_url, args.access_key_id, args.secret_key_id)
    if s3 == None:
         resp_error(args.cmd, None)

if args.cmd == 'keygen':
    resp = request_admin(args.cmd, {"namespace": args.namespace,
                                    "lifetime": args.lifetime,
                                    "bucket": args.name })
    if resp != None and resp.status == 200:
        akey = json.loads(resp.read().decode('utf-8'))
        print("Access Key %s\nSecret Key %s" % \
              (akey['access-key-id'], akey['access-key-secret']))
        if args.save == True:
            args.access_key_id = akey['access-key-id']
            args.secret_key_id =  akey['access-key-secret']
            saveCreds(args)
    else:
        resp_error(args.cmd, resp)

if args.cmd == 'keydel':
    resp = request_admin(args.cmd, {"access-key-id": args.access_key_id})
    if resp != None and resp.status == 200:
        print("Access Key %s deleted" % (args.access_key_id))
    else:
        resp_error(args.cmd, resp)

if args.cmd == 'notify':
    resp = request_notify({'namespace': args.namespace, 'bucket': args.bucket, 'ops': 'put', 'queue': args.queue})
    if resp != None and resp.status == 202:
        print("Notification set up")
    else:
        resp_error("notify", resp)

if args.cmd == 'list-buckets':
    try:
        resp = s3.list_buckets()
        print("Buckets list")
        print("\tOwner: DisplayName '%s' ID '%s'" % \
              (resp['Owner']['DisplayName'], resp['Owner']['ID']))
        if 'Buckets' in resp:
            for x in resp['Buckets']:
                if 'Name' in x:
                    print("\tBucket: Name %s CreationDate %s" % \
                          (x['Name'], x['CreationDate']))
    except:
        print("ERROR: Can't list bucket")

if args.cmd == 'list-objects':
    resp = s3.list_objects_v2(Bucket = args.name)
    print("Objects list (bucket %s count %d)" % (args.name, resp['KeyCount']))
    if 'Contents' in resp:
        for x in resp['Contents']:
            print("\tObject: Key %s Size %d" % (x['Key'], x['Size']))

if args.cmd == 'list-uploads':
    try:
        resp = s3.list_multipart_uploads(Bucket = args.name)
        print("Bucket %s uploads list" % (args.name))
        if 'Uploads' in resp:
            for x in resp['Uploads']:
                print("\tKey %s Initiated %s UploadId %s" % \
                      (x['Key'], x['Initiated'], x['UploadId']))
    except:
        print("ERROR: Can't list uploads")

if args.cmd == 'bucket-add':
    if args.name == None:
        args.name = genBucketName()
    print("Creating bucket %s" % (args.name))
    try:
        resp = s3.create_bucket(Bucket = args.name)
        print("\tdone")
    except:
        print("ERROR: Can't create bucket")

if args.cmd == 'bucket-del':
    print("Deleting bucket %s" % (args.name))
    try:
        resp = s3.delete_bucket(Bucket = args.name)
        print("\tDone")
    except:
        print("ERROR: Can't delete bucket")

if args.cmd == 'folder-add':
    if args.key == None:
        args.key = genObjectName()
    args.key += "/"
    print("Creating folder %s/%s" % (args.name, args.key))
    try:
        resp = s3.put_object(Bucket = args.name, Key = args.key, Body = "")
        print("\tDone")
    except:
        print("ERROR: Can't create folder")

if args.cmd == 'object-add':
    if args.key == None:
        args.key = genObjectName()
    if args.file == None:
        body = genRandomData(64)
    else:
        with open(args.file, 'rb') as f:
            body = f.read()
            f.close()
    print("Creating object %s/%s" % (args.name, args.key))
    try:
        resp = s3.put_object(Bucket = args.name, Key = args.key, Body = body)
        print("\tDone")
    except:
        print("ERROR: Can't create object")

if args.cmd == 'object-copy':
    print("Copying object %s/%s -> %s/%s" % \
          (args.name, args.key, args.dst_name, args.dst_key))
    try:
        resp = s3.copy_object(Bucket = args.name, Key = args.key,
                              CopySource = {'Bucket': args.dst_name,
                                           'Key': args.dst_key})
        print("\tDone")
    except:
        print("ERROR: Can't copy object")

if args.cmd == 'object-del':
    print("Deleting object %s/%s" % (args.name, args.key))
    try:
        resp = s3.delete_object(Bucket = args.name, Key = args.key)
        print("\tDone")
    except:
        print("ERROR: Can't delete object")

if args.cmd == 'object-part-init':
    if args.key == None:
        args.key = genObjectName()
    print("Initiating multipart upload %s/%s" % (args.name, args.key))
    try:
        resp = s3.create_multipart_upload(Bucket = args.name, Key = args.key)
        print("\tUploadID: %s" % (resp['UploadId']))
    except:
        print("ERROR: Can't initiate multipart upload")

if args.cmd == 'object-part-fini':
    if args.key == None:
        args.key = genObjectName()
    print("Finalizing multipart upload %s/%s" % (args.name, args.key))
    try:
        parts = []
        for x in args.list.split(","):
            y = x.split(":")
            parts.append({'PartNumber': int(y[0]), 'ETag': y[1]})
        resp = s3.complete_multipart_upload(Bucket = args.name, Key = args.key,
                                            MultipartUpload = {'Parts': parts},
                                            UploadId = args.id)
        if 'Bucket' in resp:
            print("\tBucket %s Key %s ETag %s" % (resp['Bucket'], resp['Key'], resp['ETag']))
    except:
        print("ERROR: Can't finalize multipart upload")

if args.cmd == 'object-part-list':
    print("List uploading parts %s" % (args.name))
    try:
        resp = s3.list_parts(Bucket = args.name,
                             Key = args.key,
                             UploadId = args.id)
        if 'UploadId' in resp:
            print("\tParts %s/%s/%s" % (resp['Bucket'], resp['Key'], resp['UploadId']))
            if 'Parts' in resp:
                for x in resp['Parts']:
                    print("\t\tPartNumber %4d Size %6d ETag %s LastModified %s" % \
                          (x['PartNumber'], x['Size'], x['ETag'], x['LastModified']))
    except:
        print("ERROR: Can't list parts")

if args.cmd == 'object-part-add':
    print("Upload part %s/%s/%s/%s" % (args.name, args.key, args.id, args.part))
    if args.file == None:
        body = genRandomData(64)
    else:
        with open(args.file, 'rb') as f:
            body = f.read()
            f.close()
    try:
        resp = s3.upload_part(Bucket = args.name,
                              Key = args.key,
                              PartNumber = int(args.part),
                              UploadId = args.id,
                              Body = body)
        print("\tETag: %s" % (resp['ETag']))
    except:
        print("ERROR: Can't upload part")

if args.cmd == 'object-part-abort':
    print("Aborting multipart upload %s/%s/%s" % (args.name, args.key, args.id))
    try:
        resp = s3.abort_multipart_upload(Bucket = args.name,
                                         Key = args.key,
                                         UploadId = args.id)
        print("\tDone")
    except:
        print("ERROR: Can't initiate multipart upload")
