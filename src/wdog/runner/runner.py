#
# © 2018 SwiftyCloud OÜ. All rights reserved.
# Info: info@swifty.cloud
#

import os
import sys
import socket
import json
import importlib.util
import time
import traceback

swyres = None
swytb = ""
swycode = None

try:
    sys.path += [ "/packages" + p for p in sys.path ]
    spec = importlib.util.spec_from_file_location('code', '/function/' + sys.argv[1] + '.py')
    swycode = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(swycode)
except:
    swycode = None
    etype, value, tb = sys.exc_info()
    swyres = "2:Error loading script (%s)" % value
    swytb = ''.join(traceback.format_exception(etype, value, tb))

q = socket.fromfd(3, socket.AF_UNIX, socket.SOCK_SEQPACKET)
os.close(3)

def readmsg(sk):
    data = b''
    while True:
        c = q.recv(1024)
        data += c
        if len(c) < 1024:
            return data

def sendmsg(sk, msg):
    while len(msg) > 0:
        s = msg[:1024]
        q.send(s)
        msg = msg[1024:]


while True:
    data = readmsg(q)

    if swycode != None:
        rq = json.loads(data)
        if not "content" in rq:
            rq["content"] = "text/plain"
        req = type('request', (object,), rq)
        try:
            if req.content == "application/json":
                try:
                    b = json.loads(req.body)
                except:
                    pass
                else:
                    req.b = type('body', (object,), b)

            res, resb = swycode.Main(req)
            res = { "res": 0, "ret": json.dumps(res) }
            if resb != None:
                try:
                    res["status"] = int(resb["status"])
                except:
                    pass
        except:
            print("Exception running FN:")
            traceback.print_exc()
            res = { "res": 1, "ret": "Exception" }
    else:
        print(swytb)
        res = { "res": 2, "ret": swyres }

    sendmsg(q, json.dumps(res).encode('utf-8'))
