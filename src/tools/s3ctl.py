import http.client
import argparse
import urllib
import json
import sys

def hmac_sha256(key, msg):
    hash_obj = HMAC.new(key=key, msg=msg, digestmod=SHA256)
    return hash_obj.hexdigest()

parser = argparse.ArgumentParser(prog='s3ctl.py')
parser.add_argument('--admin-secret', dest = 'admin_secret',
                    default = '',
                    help = 'access token to ented admin interface')
parser.add_argument('--endpoint-url', dest = 'endpoint_url',
                    default = '192.168.122.197:8787',
                    help = 'S3 service address')

sp = parser.add_subparsers(dest = 'cmd')
for cmd in ['akey-gen', 'bucket-gen']:
    sp.add_parser(cmd)

for cmd in ['akey-del', 'bucket-del']:
    spp = sp.add_parser(cmd)
    spp.add_argument('name')

args = parser.parse_args()

if args.cmd == None:
    parser.print_help()
    sys.exit(1)

def resp_error(cmd, resp):
    print("Command '%s' failed %d with body %s" % \
          (cmd, resp.status, resp.read().decode('utf-8')))
    sys.exit(1)

if args.cmd == 'akey-gen':
    params = urllib.parse.urlencode({'cmd': args.cmd})
    headers = {"x-swy-secret": args.admin_secret}
    conn = http.client.HTTPConnection(args.endpoint_url)
    conn.request('GET','/v1/api/admin?' + params, None, headers)
    resp = conn.getresponse()
    if resp.status == 200:
        akey = json.loads(resp.read().decode('utf-8'))
        print("Access Key %s\nSecret Key %s" % \
              (akey['access-key-id'], akey['access-key-secret']))
    else:
        resp_error(args.cmd, resp)
