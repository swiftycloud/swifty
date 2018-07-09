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
#    curl -X PUT -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL' -H 'Content-type: application/json' -d '$STRING'
# -. Get user image
#    curl -X GET -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL'
#    THe result of this call is '{ "img": $STRING }' JSON.
#
# Swifty FN API doesn't yet allow to pass binary data between request/responce
# bodies and function code, so we recommend you base64-encode your image before
# putting into this FN.
#

import boto3
import os
import json
import base64

def Main(req):
    addr = os.getenv('MWARE_S3IMAGES_ADDR')
    akey = os.getenv('MWARE_S3IMAGES_KEY')
    asec = os.getenv('MWARE_S3IMAGES_SECRET')

    s3 = boto3.session.Session().client(service_name = 's3',
            aws_access_key_id = akey, aws_secret_access_key = asec, endpoint_url = 'http://' + addr + '/')

    if req.method == 'POST':
        body = base64.b64decode(req.body)
        s3.put_object(Bucket = 'images', Key = req.claims['cookie'], Body = body)
        return 'OK', None

    if req.method == 'GET':
        resp = s3.get_object(Bucket = 'images', Key = req.claims['cookie'])
        body = base64.b64encode(resp['Body'].read()).decode('utf-8')
        return { 'img': body }, None

    if req.method == 'DELETE':
        s3.delete_object(Bucket = 'images', Key = req.claims['cookie'])
        return 'OK', None

    return 'ERROR', { 'status': 503 }
