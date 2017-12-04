import http.client
import argparse
import random
import string
import urllib
import boto3
import json
import sys

#[cyrill@uranus swifty] python3 src/tools/s3ctl.py --endpoint-url 127.0.0.1:8786 keygen --namespace s3
#Access Key 2Q9WBU2DSYRO4310IXSQ
#Secret Key mAQQFIhf4SZWRP2zptSlGxYiOMd3MH9H5FpMSFJx

# sha256 for 'this is admin token' string
default_admin_token = "44b56e701117c2ef5f116e6b8d6df7bb070e9068bd06d794cac3ae8d672bf345"

def genRandomData(len):
    return ''.join(random.SystemRandom().choice(string.ascii_uppercase + string.digits) for _ in range(len))

def genBucketName():
    return genRandomData(6)

def genObjectName():
    return genRandomData(10)

parser = argparse.ArgumentParser(prog='s3ctl.py')
parser.add_argument('--admin-secret', dest = 'admin_secret',
                    default = default_admin_token,
                    help = 'access token to ented admin interface')
parser.add_argument('--endpoint-url', dest = 'endpoint_url',
                    default = '192.168.122.197:8787',
                    help = 'S3 service address')

sp = parser.add_subparsers(dest = 'cmd')
for cmd in ['keygen']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--namespace', dest = 'namespace', required = True)

for cmd in ['keydel']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--access-key-id', dest = 'access_key_id', required = True)

for cmd in ['bucket-add']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--access-key-id', dest = 'access_key_id', required = True)
    spp.add_argument('--secret-key-id', dest = 'secret_key_id', required = True)
    spp.add_argument('--name', dest = 'name', required = False)

for cmd in ['bucket-del']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--access-key-id', dest = 'access_key_id', required = True)
    spp.add_argument('--secret-key-id', dest = 'secret_key_id', required = True)
    spp.add_argument('--name', dest = 'name', required = True)

for cmd in ['object-add']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--access-key-id', dest = 'access_key_id', required = True)
    spp.add_argument('--secret-key-id', dest = 'secret_key_id', required = True)
    spp.add_argument('--name', dest = 'name', required = True)
    spp.add_argument('--key', dest = 'key', required = False)
    spp.add_argument('--file', dest = 'file', required = False)

for cmd in ['object-del']:
    spp = sp.add_parser(cmd)
    spp.add_argument('--access-key-id', dest = 'access_key_id', required = True)
    spp.add_argument('--secret-key-id', dest = 'secret_key_id', required = True)
    spp.add_argument('--name', dest = 'name', required = True)
    spp.add_argument('--key', dest = 'key', required = True)

args = parser.parse_args()

if args.cmd == None:
    parser.print_help()
    sys.exit(1)

def resp_error(cmd, resp):
    if resp != None:
        print("ERROR: Command '%s' failed %d with: %s" % \
            (cmd, resp.status, resp.read().decode('utf-8')))
    else:
        print("ERROR: Command '%s' failed" % (cmd))
    sys.exit(1)

def request_admin(cmd, data):
    params = urllib.parse.urlencode({'cmd': args.cmd})
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

if args.cmd == 'keygen':
    resp = request_admin(args.cmd, {"namespace": args.namespace})
    if resp != None and resp.status == 200:
        akey = json.loads(resp.read().decode('utf-8'))
        print("Access Key %s\nSecret Key %s" % \
              (akey['access-key-id'], akey['access-key-secret']))
    else:
        resp_error(args.cmd, resp)
elif args.cmd == 'keydel':
    resp = request_admin(args.cmd, {"access-key-id": args.access_key_id})
    if resp != None and resp.status == 200:
        print("Access Key %s deleted" % (args.access_key_id))
    else:
        resp_error(args.cmd, resp)
elif args.cmd == 'bucket-add':
    s3 = make_s3(args.endpoint_url, args.access_key_id, args.secret_key_id)
    if s3 == None:
         resp_error(args.cmd, None)
    if args.name == None:
        args.name = genBucketName()
    print("Creating bucket %s" % (args.name))
    try:
        resp = s3.create_bucket(Bucket = args.name)
        print("\tdone")
    except:
        print("ERROR: Can't create bucket")
elif args.cmd == 'bucket-del':
    s3 = make_s3(args.endpoint_url, args.access_key_id, args.secret_key_id)
    if s3 == None:
         resp_error(args.cmd, None)
    print("Deleting bucket %s" % (args.name))
    try:
        resp = s3.delete_bucket(Bucket = args.name)
        print("\tDone")
    except:
        print("ERROR: Can't delete bucket")
elif args.cmd == 'object-add':
    s3 = make_s3(args.endpoint_url, args.access_key_id, args.secret_key_id)
    if s3 == None:
         resp_error(args.cmd, None)
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
elif args.cmd == 'object-del':
    s3 = make_s3(args.endpoint_url, args.access_key_id, args.secret_key_id)
    if s3 == None:
         resp_error(args.cmd, None)
    print("Deleting object %s/%s" % (args.name, args.key))
    try:
        resp = s3.delete_object(Bucket = args.name, Key = args.key)
        print("\tDone")
    except:
        print("ERROR: Can't delete object")
