import postgresql
import os

dbconn = None

def gcon(req):
    global dbconn
    if dbconn == None:
        print("New connection")
        mwn = req.args['dbname'].upper()
        dbaddr = os.getenv('MWARE_' + mwn + '_ADDR')
        dbuser = os.getenv('MWARE_' + mwn + '_USER')
        dbpass = os.getenv('MWARE_' + mwn + '_PASS')
        dbname = os.getenv('MWARE_' + mwn + '_DBNAME')
        connstr = 'pq://%s:%s@%s/%s' % (dbuser, dbpass, dbaddr, dbname)
        dbconn = postgresql.open(connstr)
    else:
        print("Reuse connection")
    return dbconn

def main(req.args):
    db = gcon(req.args)
    res = "invalid"
    if req.args['action'] == 'create':
        db.execute("CREATE TABLE data (id SERIAL PRIMARY KEY, key CHAR(64), val CHAR(64))")
        res = "done"
    elif req.args['action'] == 'insert':
        ins = db.prepare("INSERT INTO data (key, val) VALUES ($1, $2)")
        ins(req.args['key'], req.args['val'])
        res = "done"
    elif req.args['action'] == 'select':
        sel = db.prepare("SELECT val FROM data WHERE key = $1")
        res = sel(req.args['key'])

    return { "res": res }, None
