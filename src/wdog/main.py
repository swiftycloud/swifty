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

def runner(runq, resq):
    while True:
        args = runq.get()
        res = swycode.main(args)
        resq.put(res)

runq = Queue()
resq = Queue()
runp = Process(target = runner, args = (runq, resq))
runp.start()

class SwyHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        print("PING: %s" % self.path)
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        global runq
        global resq
        global runp

        if self.path != '/v1/run':
            print(self.path)
            self.send_response(404)
            self.end_headers()
            return

        try:
            clen = int(self.headers.get('content-length'))
            body = self.rfile.read(clen)
            req = json.loads(body)

            if req['podtoken'] != swyfunc['podtoken']:
                raise Exception("POD token mismatch")

            print("Call with args: %r" % req['args'])
            runq.put(req['args'])
            try:
                res = resq.get(timeout = float(swyfunc['timeout']))
                print("Result: %r" % res)
            except queue.Empty as ex:
                print("Timeout running FN")
                os.kill(runp.pid, signal.SIGKILL)
                runp.join()
                # Restart everything, including queues, don't want them
                # to contain dangling trash from previous runs
                runq = Queue()
                resq = Queue()
                runp = Process(target = runner, args = (runq, resq))
                runp.start()
                raise Exception("Function run timeout!")

            resb = json.dumps(res)
            ret = { 'return': resb, 'code': 0, 'stdout': "", 'stderr': "" }
            retb = json.dumps(ret).encode('utf-8')
        except Exception as e:
            print("*** Error processing request ***")
            traceback.print_exc()
            print("********************************")
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
