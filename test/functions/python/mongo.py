from pymongo import MongoClient
import os
import swifty

def main(args):
    db = swifty.MongoDatabase(args['dbname'])
    colname = args['collection']
    col = db[colname]

    if args['action'] == 'insert':
        col.insert_one({ "key": args['key'], "val": args['val'] })
        return { "res": "done" }

    if args['action'] == 'select':
        res = col.find_one({ "key": args['key'] })
        return { "res": res['val'] }

    return { "res": "invalid" }
