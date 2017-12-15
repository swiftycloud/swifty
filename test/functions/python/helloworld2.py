def main(args):
    print("called with: %s" % args['name'])
    return {"message": "hw2:python:%s" % args['name']}
