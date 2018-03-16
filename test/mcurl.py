#!/usr/bin/env python3

import http.client
import sys
import time
from multiprocessing import Process

def curl(nr, d, url):
    ers = {}
    oks = 0
    minlat = 100.0
    maxlat = 0.0
    gist = [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]
    for i in range(0, nr):
        conn = http.client.HTTPConnection(url[2])
        s = time.time()
        conn.request('POST', '/' + url[3], body = "{}")
        resp = conn.getresponse()
        lat = time.time() - s
        if lat < minlat:
            minlat = lat
        elif lat > maxlat:
            maxlat = lat
        lms = int(lat * 1000)
        if lms < 15:
            gist[lms] += 1
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
    return minlat, maxlat, gist

url = sys.argv[1]
nr = int(sys.argv[2])
p = int(sys.argv[3])
d = float(sys.argv[4])

print("Call %s %d times %.2f delay %d threads" % (url, nr, d, p))

url = url.split('/', 3)
if p == 1:
    start = time.time()
    mn, mx, gist = curl(nr, d, url)
    dur = time.time() - start
    print("%.2f seconds" % dur)
    print("%.2f msec call lat (%.2f ... %.2f)" % (dur * 1000 / nr, mn * 1000, mx * 1000))
    print("Gist: %r" % gist)
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
