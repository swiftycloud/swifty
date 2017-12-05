#!/usr/local/bin/python3 -u
from multiprocessing import Process, Queue
from http.server import BaseHTTPRequestHandler, HTTPServer
import os
import signal
import json
import importlib.util
import sys
import traceback

spec = importlib.util.spec_from_file_location('code', '/function/code/script.py')
swycode = importlib.util.module_from_spec(spec)
spec.loader.exec_module(swycode)

def runMain(res, args):
    res.put(swycode.main(args))

class SwyHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        print("PING: %s" % self.path)
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        print(self.path)
        if self.path != '/v1/run':
            self.send_response(404)
            self.end_headers()
            return

        try:
            clen = int(self.headers.get('content-length'))
            body = self.rfile.read(clen)
            req = json.loads(body)

            if req['podtoken'] != swyfunc['podtoken']:
                raise Exception("POD token mismatch")

            result = Queue()

            print("Call with args: %r" % req['args'])
            tsk = Process(target = runMain, args = (result, req['args']))
            tsk.start()
            tsk.join(float(swyfunc['timeout']))
            if tsk.is_alive():
                os.kill(tsk.pid, signal.SIGKILL)
                tsk.join()
                raise Exception("Timeout!")

            if result.empty():
                raise Exception("No object returned")
            if tsk.exitcode != 0:
                raise Exception("Function didn't terminate smoothly")

            res = result.get()
            resb = json.dumps(res)

            ret = { 'return': resb, 'code': 0, 'stdout': "", 'stderr': "" }
            retb = json.dumps(ret).encode('utf-8')
        except Exception as e:
            print("Error processing request")
            traceback.print_exc()
            self.send_response(400)
            self.end_headers()
        else:
            self.send_response(200)
            self.send_header('Content-type', 'application/json')
            self.end_headers()
            self.wfile.write(retb)

fdesc = os.getenv('SWD_FUNCTION_DESC')
if not fdesc:
    raise Exception("No function desc provided")

swyfunc = json.loads(fdesc)

os.chdir(swyfunc['dir'])

addr = os.getenv('SWD_POD_IP')
port = os.getenv('SWD_PORT')
if not addr or not port:
    raise Exception("No IP:PORT pair (%s:%s)" % (addr, port))

print("Listen on %s:%s" % (addr, port))
http = HTTPServer((addr, int(port)), SwyHandler)
http.serve_forever()
