#
# Sample user avatar management function that uses S3 and swifty JWT auth.
#
# How to use:
#
# 1. Create authentication-as-a-service
# 2. Create "images" bucket in S3
# 3. Register and configure this function
#    - add call authentication from step 1
#    - add "images" bucket from step 2
#    - add "url" event trigger (of any name)
#
# Next, a user should authenticate.
#
# 1. Signup a user
#    curl -X POST '$AUTH_FN_URL?action=signup&userid=$NAME&password=$PASS'
# 2. Sign in and grab the JWT
#    curl -X POST '$AUTH_FN_URL?action=signin&userid=$NAME&password=$PASS'
#
# Now call this FN with obtained JWT
#
# -. Put user image
#    curl -X POST -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL?action=put' -d '$STRING'
# -. Get user image
#    curl -X POST -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL?action=get'
#    THe result of this call is '{ "img": $STRING }' JSON.
#
# Swifty FN API doesn't yet allow to pass binary data between request/responce
# bodies and function code, so we recommend you base64-encode your image before
# putting into this FN.
#

import boto3
import os
import json

def main(args):
    addr = os.getenv('MWARE_S3IMAGES_ADDR')
    akey = os.getenv('MWARE_S3IMAGES_KEY')
    asec = os.getenv('MWARE_S3IMAGES_SECRET')

    s3 = boto3.session.Session().client(service_name = 's3',
            aws_access_key_id = akey, aws_secret_access_key = asec, endpoint_url = 'http://' + addr + '/')

    claims = json.loads(args['_SWY_JWT_CLAIMS_'])

    if args['action'] == 'put':
        s3.put_object(Bucket = 'images', Key = claims['cookie'], Body = args['_SWY_BODY_'])
        return 'OK'

    if args['action'] == 'get':
        resp = s3.get_object(Bucket = 'images', Key = claims['cookie'])
        if resp['ContentLength'] <= 0:
            return 'ERROR'

        return { 'img': resp['Body'].read().decode('utf-8') }

    if args['action'] == 'del':
        s3.delete_object(Bucket = 'images', Key = claims['cookie'])
        return 'OK'

    return 'ERROR'
