import boto3
import os

def main(args):
    mwn = args['bucket'].upper()
    addr = os.getenv('MWARE_' + mwn + '_ADDR')
    akey = os.getenv('MWARE_' + mwn + '_S3KEY')
    asec = os.getenv('MWARE_' + mwn + '_S3SEC')

    s3 = boto3.session.Session().client(service_name = 's3',
            aws_access_key_id = akey, aws_secret_access_key = asec, endpoint_url = 'http://' + addr + '/')

    if args['action'] == 'put':
        s3.put_object(Bucket = args['bucket'], Key = args['name'], Body = args['data'])
        res = 'done'
    elif args['action'] == 'get':
        resp = s3.get_object(Bucket = args['bucket'], Key = args['name'])
        if resp['ContentLength'] > 0:
            res = resp['Body'].read().decode("utf-8")
        else:
            res = 'error'
    else:
        res = 'error'

    return { "res": res }
