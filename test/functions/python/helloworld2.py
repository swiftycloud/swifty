def main(req):
    print("called with: %s" % req.args['name'])
    return {"message": "hw2:python:%s" % req.args['name']}, None
