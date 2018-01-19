import pymysql.cursors
import os

_swiftyMariaConn = None

def MariaConn(mwname):
    global _swiftyMariaConn
    if _swiftyMariaConn == None:
        mwn = mwname.upper()
        x = os.getenv('MWARE_' + mwn + '_ADDR').split(":")
        dbaddr = x[0]
        dbport = int(x[1])
        dbuser = os.getenv('MWARE_' + mwn + '_USER')
        dbpass = os.getenv('MWARE_' + mwn + '_PASS')
        dbname = os.getenv('MWARE_' + mwn + '_DBNAME')
        _swiftyMariaConn = pymysql.connect(host=dbaddr, port=dbport,
                user=dbuser, password=dbpass, db=dbname,
                cursorclass=pymysql.cursors.DictCursor)

    return _swiftyMariaConn
