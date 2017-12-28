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

swyfunc = {}

swyfunc['podtoken'] = os.getenv('SWD_POD_TOKEN')
if not swyfunc['podtoken']:
    raise Exception("No podtoken provided")
swyfunc['timeout'] = os.getenv('SWD_FN_TMO')
if not swyfunc['timeout']:
    raise Exception("No fn timeout provided")

addr = os.getenv('SWD_POD_IP')
port = os.getenv('SWD_PORT')
if not addr or not port:
    raise Exception("No IP:PORT pair (%s:%s)" % (addr, port))

def durusec(start):
    return int((time.time() - start) * 1000000)

def runnerFn(runq, resq, pout, perr):
    global swycode

    os.dup2(pout, 1)
    os.close(pout)
    os.dup2(perr, 2)
    os.close(perr)
    while True:
        args = runq.get()
        try:
            now = time.time()
            res = swycode.main(args)
            dur = durusec(now)
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
    def _makeQnP(self):
        self.runq = Queue()
        self.resq = Queue()
        self.runp = Process(target = runnerFn, args = (self.runq, self.resq, self.pout, self.perr))
        self.waiter = Thread(target = waiterFn, args = (self.runp, self.resq))

    def __init__(self, fdesc):
        self.fntmo = float(fdesc['timeout']) / 1000
        pin, self.pout = os.pipe() # pout is kept as FD for restart()-s
        self.pin = nbfile(pin)
        pin, self.perr = os.pipe() # so id perr
        self.pine = nbfile(pin)
        self._makeQnP()

    def start(self):
        self.runp.start()
        self.waiter.start()
        print("Started subp: %d" % self.runp.pid)

    def _restart(self):
        os.kill(self.runp.pid, signal.SIGKILL)
        self.waiter.join()
        # Flush messages
        self.stdout()
        self.stderr()
        # Restart everything, including queues, don't want them
        # to contain dangling trash from previous runs
        self._makeQnP()
        self.start()

    def try_call_fn(self, start, args):
        print("Call with args: %r" % args)
        self.runq.put(args)
        try:
            res = self.resq.get(self.fntmo)
            fout = "".join(self.pin.readlines())
            ferr = "".join(self.pine.readlines())
            print("Result: %s" % res)
            print("Out:    %s" % fout)
        except queue.Empty as ex:
            print("Timeout running FN")
            self._restart()
            return { 'return': "timeout", 'code': 524 }

        if res["res"] != "ok":
            print("Error running FN")
            return { 'return': res["res"], 'code': 500 }

        return { 'return': res["retj"],
                'code': 0,
                'stdout': fout,
                'stderr': ferr,
                'time': res["time"],
                'ctime': durusec(start),
        }

class FailRunner:
    def __init__(self, etype, value, tb):
        self.exc_txt = "%s" % value
        self.exc_msg = ''.join(traceback.format_exception(etype, value, tb))

    def try_call_fn(self, start, args):
        return { 'code': 503, 'return': self.exc_txt, 'stdout': self.exc_msg, }

try:
    spec = importlib.util.spec_from_file_location('code', '/function/script.py')
    swycode = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(swycode)
except:
    etype, value, tb = sys.exc_info()
    runner = FailRunner(etype, value, tb)
else:
    runner = Runner(swyfunc)
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
            start = time.time()
            clen = int(self.headers.get('content-length'))
            body = self.rfile.read(clen)
            req = json.loads(body)

            if req['podtoken'] != swyfunc['podtoken']:
                raise Exception("POD token mismatch")

            ret = runner.try_call_fn(start, req['args'])
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

print("Listen on %s:%s" % (addr, port))
http = HTTPServer((addr, int(port)), SwyHandler)
http.serve_forever()
