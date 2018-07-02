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
        print(data)
        req = type('request', (object,), json.loads(data))
        try:
            res, resb = swycode.Main(req)
            res = { "res": 0, "ret": json.dumps(res) }
        except:
            print("Exception running FN:")
            traceback.print_exc()
            res = { "res": 1, "ret": "Exception" }
    else:
        print(swytb)
        res = { "res": 2, "ret": swyres }

    sendmsg(q, json.dumps(res).encode('utf-8'))
