def main(req):
    print("Object %s created in bucket %s" % (req.args['object'], req.args['bucket']))
    return {}, None
