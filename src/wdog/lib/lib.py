import pymysql.cursors
from pymongo import MongoClient
import os

_swiftyMariaConns = {}

def MariaConn(mwname):
    global _swiftyMariaConns
    conn = _swiftyMariaConns.get(mwname, None)
    if conn == None:
        mwn = mwname.upper()
        x = os.getenv('MWARE_MARIA' + mwn + '_ADDR')
        if x == None:
            raise Exception("Middleware not attached")
        x = x.split(":")
        dbaddr = x[0]
        dbport = int(x[1])
        dbuser = os.getenv('MWARE_MARIA' + mwn + '_USER')
        dbpass = os.getenv('MWARE_MARIA' + mwn + '_PASS')
        dbname = os.getenv('MWARE_MARIA' + mwn + '_DBNAME')
        conn = pymysql.connect(host=dbaddr, port=dbport,
                user=dbuser, password=dbpass, db=dbname,
                cursorclass=pymysql.cursors.DictCursor)
        _swiftyMariaConns[mwname] = conn

    return conn

_swiftyMongoClients = {}

def MongoDatabase(mwname):
    global _swiftyMongoClients
    mwn = mwname.upper()
    dbname = os.getenv('MWARE_MONGO' + mwn + '_DBNAME')
    if dbname == None:
        raise Exception("Middleware not attached")
    clnt = _swiftyMongoClients.get(mwname, None)
    if clnt == None:
        dbaddr = os.getenv('MWARE_MONGO' + mwn + '_ADDR')
        dbuser = os.getenv('MWARE_MONGO' + mwn + '_USER')
        dbpass = os.getenv('MWARE_MONGO' + mwn + '_PASS')
        connstr = 'mongodb://%s:%s@%s/%s' % (dbuser, dbpass, dbaddr, dbname)
        clnt = MongoClient(connstr)
        _swiftyMongoClients[mwname] = clnt

    return clnt[dbname]
