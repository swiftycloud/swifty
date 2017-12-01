import http.client
import argparse
import urllib
import json
import sys

# sha256 for 'this is admin token' string
default_admin_token = "44b56e701117c2ef5f116e6b8d6df7bb070e9068bd06d794cac3ae8d672bf345"

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

args = parser.parse_args()

if args.cmd == None:
    parser.print_help()
    sys.exit(1)

def resp_error(cmd, resp):
    print("Command '%s' failed %d with: %s" % \
          (cmd, resp.status, resp.read().decode('utf-8')))
    sys.exit(1)

def request(cmd, data):
    params = urllib.parse.urlencode({'cmd': args.cmd})
    headers = {"x-swy-secret": args.admin_secret,
               'Content-type': 'application/json'}
    conn = http.client.HTTPConnection(args.endpoint_url)
    conn.request('POST','/v1/api/admin/' + args.cmd, json.dumps(data), headers)
    return conn.getresponse()

if args.cmd == 'keygen':
    resp = request(args.cmd, {"namespace": args.namespace})
    if resp.status == 200:
        akey = json.loads(resp.read().decode('utf-8'))
        print("Access Key %s\nSecret Key %s" % \
              (akey['access-key-id'], akey['access-key-secret']))
    else:
        resp_error(args.cmd, resp)
elif args.cmd == 'keydel':
    resp = request(args.cmd, {"access-key-id": args.access_key_id})
    if resp.status == 200:
        print("Access Key %s deleted" % (args.access_key_id))
    else:
        resp_error(args.cmd, resp)
