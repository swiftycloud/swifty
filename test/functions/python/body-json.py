import json

def main(args):
    o = json.loads(args["_SWY_BODY_"].encode())
    return { "status": o["status"] }
