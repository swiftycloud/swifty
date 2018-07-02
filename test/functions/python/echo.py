def Main(req):
    print(req)
    return {"name": req.args["name"], "method": req.method}
