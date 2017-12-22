#!/usr/local/bin/python3 -u
from multiprocessing import Process, Queue
from threading import Thread
from http.server import BaseHTTPRequestHandler, HTTPServer
import queue
import os
import signal
import json
import importlib.util
import sys
import traceback
import fcntl
import time

spec = importlib.util.spec_from_file_location('code', '/function/script.py')
swycode = importlib.util.module_from_spec(spec)
spec.loader.exec_module(swycode)

def runnerFn(runq, resq, pout, perr):
    os.dup2(pout, 1)
    os.close(pout)
    os.dup2(perr, 2)
    os.close(perr)
    while True:
        args = runq.get()
        try:
            now = time.time()
            res = swycode.main(args)
            dur = int((time.time() - now) * 1000000) # to usec
        except:
            print("Exception running FN:")
            traceback.print_exc()
            resq.put({"res": "exception"})
        else:
            resq.put({"res": "ok", "retj": json.dumps(res), "time": dur})

# Main process waits on the resq, but if the runner process
# exits for some reason, the former will get blocked till timeout.
# Not nice, let's join it and report empty return instead.

def waiterFn(subp, resq):
    subp.join()
    print("Runner exited")
    resq.put({"res": "exited"})

def nbfile(fd):
        fl = fcntl.fcntl(fd, fcntl.F_GETFL)
        fcntl.fcntl(fd, fcntl.F_SETFL, fl | os.O_NONBLOCK)
        return os.fdopen(fd)

class Runner:
    def makeQnP(self):
        self.runq = Queue()
        self.resq = Queue()
        self.runp = Process(target = runnerFn, args = (self.runq, self.resq, self.pout, self.perr))
        self.waiter = Thread(target = waiterFn, args = (self.runp, self.resq))

    def __init__(self):
        pin, self.pout = os.pipe() # pout is kept as FD for restart()-s
        self.pin = nbfile(pin)
        pin, self.perr = os.pipe() # so id perr
        self.pine = nbfile(pin)
        self.makeQnP()

    def start(self):
        self.runp.start()
        self.waiter.start()
        print("Started subp: %d" % self.runp.pid)

    def restart(self):
        os.kill(self.runp.pid, signal.SIGKILL)
        self.waiter.join()
        # Flush messages
        self.stdout()
        self.stderr()
        # Restart everything, including queues, don't want them
        # to contain dangling trash from previous runs
        self.makeQnP()
        self.start()

    def stdout(self):
        return "".join(self.pin.readlines())

    def stderr(self):
        return "".join(self.pine.readlines())


runner = Runner()
runner.start()

class SwyHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        print("PING: %s" % self.path)
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        global runner

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
            runner.runq.put(req['args'])
            errc = 500
            try:
                res = runner.resq.get(timeout = float(swyfunc['timeout']) / 1000)
                fout = runner.stdout()
                ferr = runner.stderr()
                print("Result: %s" % res)
                print("Out:    %s" % fout)
            except queue.Empty as ex:
                print("Timeout running FN")
                runner.restart()
                res['res'] = "timeout"
                errc = 524

            if res["res"] == "ok":
                ret = { 'return': res["retj"], 'code': 0, 'stdout': fout, 'stderr': ferr, 'time': res["time"] }
            else:
                ret = { 'return': res["res"], 'code': errc }
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

addr = os.getenv('SWD_POD_IP')
port = os.getenv('SWD_PORT')
if not addr or not port:
    raise Exception("No IP:PORT pair (%s:%s)" % (addr, port))

print("Listen on %s:%s" % (addr, port))
http = HTTPServer((addr, int(port)), SwyHandler)
http.serve_forever()
