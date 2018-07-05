import boto3
import os

def main(req):
    mwn = req.args['bucket'].upper()
    addr = os.getenv('MWARE_' + mwn + '_ADDR')
    akey = os.getenv('MWARE_' + mwn + '_S3KEY')
    asec = os.getenv('MWARE_' + mwn + '_S3SEC')

    s3 = boto3.session.Session().client(service_name = 's3',
            aws_access_key_id = akey, aws_secret_access_key = asec, endpoint_url = 'http://' + addr + '/')

    if req.args['action'] == 'put':
        s3.put_object(Bucket = req.args['bucket'], Key = req.args['name'], Body = req.args['data'])
        res = 'done'
    elif req.args['action'] == 'get':
        resp = s3.get_object(Bucket = req.args['bucket'], Key = req.args['name'])
        if resp['ContentLength'] > 0:
            res = resp['Body'].read().decode("utf-8")
        else:
            res = 'error'
    else:
        res = 'error'

    return { "res": res }, None
