#!/usr/bin/env python3

import http.client
import sys
import time
from multiprocessing import Process

def curl(nr, d, url):
    ers = {}
    oks = 0
    for i in range(0, nr):
        conn = http.client.HTTPConnection(url[2])
        conn.request('POST', '/' + url[3])
        resp = conn.getresponse()
        if resp.status == 200:
            oks += 1
        else:
            if resp.status in ers:
                ers[resp.status] += 1
            else:
                ers[resp.status] = 1
        time.sleep(d)
    if len(ers) != 0:
        m = "%d OKs" % oks
        for e in ers:
            m += ", %d/%d ERRs" % (ers[e], e)
        print(m)

url = sys.argv[1]
nr = int(sys.argv[2])
p = int(sys.argv[3])
d = float(sys.argv[4])

print("Call %s %d times %.2f delay %d threads" % (url, nr, d, p))

url = url.split('/', 3)
if p == 1:
    start = time.time()
    curl(nr, d, url)
    dur = time.time() - start
    print("%.2f seconds" % dur)
    print("%.2f msec call lat" % (dur * 1000 / nr))
else:
    ps = []
    for i in range(0, p):
        t = Process(target=curl, args=(nr, d, url))
        t.start()
        ps.append(t)

    for t in ps:
        t.join()
        print("`- done")
    print("all done")
