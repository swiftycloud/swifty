#!/usr/bin/env python3

import http.client
import sys
import time
from multiprocessing import Process

def curl(nr, url):
    ers = 0
    for i in range(0, nr):
        conn = http.client.HTTPConnection(url[2])
        conn.request('POST', '/' + url[3])
        resp = conn.getresponse()
        if resp.status != 200:
            ers += 1
    if ers != 0:
        print("`- %d/%d ERRORs" % (ers, nr))

url = sys.argv[1]
nr = int(sys.argv[2])
p = int(sys.argv[3])

print("Call %s %d times %d threads" % (url, nr, p))

url = url.split('/', 3)
if p == 1:
    start = time.time()
    curl(nr, url)
    dur = time.time() - start
    print("%.2f seconds" % dur)
    print("%.2f msec call lat" % (dur * 1000 / nr))
else:
    ps = []
    for i in range(0, p):
        t = Process(target=curl, args=(nr, url))
        t.start()
        ps.append(t)

    for t in ps:
        t.join()
        print("`- done")
    print("all done")
