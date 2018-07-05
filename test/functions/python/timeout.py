import time
def Main(rq):
    time.sleep(float(rq.args['tmo']) / 1000)
    return "slept:%s" % rq.args['tmo']
