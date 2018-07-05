import json

def main(req):
    o = json.loads(req.body.encode())
    return { "status": o["status"] }, None
