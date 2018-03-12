#!/usr/bin/env python3

import http.client
import sys
import time

url = sys.argv[1]
nr = int(sys.argv[2])

print("Call %s %d times" % (url, nr))

url = url.split('/', 3)
start = time.time()
for i in range(0, nr):
    conn = http.client.HTTPConnection(url[2])
    conn.request('POST', '/' + url[3])
    resp = conn.getresponse()
    if resp.status != 200:
        print("`- ERROR")
dur = time.time() - start
print("%.2f seconds" % dur)
print("%.2f msec call lat" % (dur * 1000 / nr))
