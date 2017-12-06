import time
def main(args):
    time.sleep(float(args['tmo']) / 1000)
    return "slept:%s" % args['tmo']
