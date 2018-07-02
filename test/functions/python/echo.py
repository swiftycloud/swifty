def Main(req):
    print(req)
    print(req.args)
    try:
        print(req.claims)
    except:
        print("no claims")
    return {"name": req.args["name"], "method": req.method, "path": req.path}
