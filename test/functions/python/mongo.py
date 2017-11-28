from pymongo import MongoClient
import os

def main(args):
    mwn = args['dbname'].upper()
    dbaddr = os.getenv('MWARE_' + mwn + '_ADDR')
    dbuser = os.getenv('MWARE_' + mwn + '_USER')
    dbpass = os.getenv('MWARE_' + mwn + '_PASS')
    dbname = os.getenv('MWARE_' + mwn + '_DBNAME')
    connstr = 'mongodb://%s:%s@%s/%s' % (dbuser, dbpass, dbaddr, dbname)
    client = MongoClient(connstr)
    db = client[dbname]
    colname = args['collection']
    col = db[colname]

    if args['action'] == 'insert':
        col.insert_one({ "key": args['key'], "val": args['val'] })
        return { "res": "done" }

    if args['action'] == 'select':
        res = col.find_one({ "key": args['key'] })
        return { "res": res['val'] }

    return { "res": "invalid" }
