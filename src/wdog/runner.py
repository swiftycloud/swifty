#!/usr/local/bin/python3 -u
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
    spec = importlib.util.spec_from_file_location('code', '/function/script.py')
    swycode = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(swycode)
except:
    swycode = None
    etype, value, tb = sys.exc_info()
    swyres = { "code": 503, "return": '%s' % value }
    swytb = ''.join(traceback.format_exception(etype, value, tb))

fd = int(sys.argv[1])
q = socket.fromfd(fd, socket.AF_UNIX, socket.SOCK_SEQPACKET)
os.close(fd)

fd = int(sys.argv[2])
os.dup2(fd, 1)
os.close(fd)

fd = int(sys.argv[3])
os.dup2(fd, 2)
os.close(fd)

def durusec(start):
    return int((time.time() - start) * 1000000)

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
        args = json.loads(data)
        try:
            now = time.time()
            res = swycode.main(args)
            dur = durusec(now)
        except:
            print("Exception running FN:")
            traceback.print_exc()
            res = { "code": 500, "return": "Exception" }
        else:
            data = json.dumps(res)
            res = {"code": 0, "return": data}
    else:
        print(swytb)
        res = swyres

    sendmsg(q, json.dumps(res).encode('utf-8'))
