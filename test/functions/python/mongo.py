from pymongo import MongoClient
import os
import swifty

def main(req):
    db = swifty.MongoDatabase(req.args['dbname'])
    colname = req.args['collection']
    col = db[colname]

    if req.args['action'] == 'insert':
        col.insert_one({ "key": req.args['key'], "val": req.args['val'] })
        return { "res": "done" }

    if req.args['action'] == 'select':
        res = col.find_one({ "key": req.args['key'] })
        return { "res": res['val'] }

    return { "res": "invalid" }, None
