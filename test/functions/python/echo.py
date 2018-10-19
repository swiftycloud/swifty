def Main(req):
    print("Object:" + " ".join(dir(req)))
    print("Ct: " + req.content)
    print("Args: " + ("%r" % req.args))
    try:
        print(req.claims)
    except:
        print("no claims")
    try:
        print(req.body)
    except:
        print("no body")
    try:
        print("B:" + ("%r" % req.b) + " :".join(dir(req.b)))
        return {"name": req.b.name }, {"status": 201}
    except:
        print("no b")
        return {"name": req.args["name"] }, {"status": 201}
