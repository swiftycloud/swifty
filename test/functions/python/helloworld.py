def Main(req):
    return {"message": "hw:python:%s" % req.args['name']}, None
