import postgresql
import os

dbconn = None

def gcon(args):
    global dbconn
    if dbconn == None:
        print("New connection")
        mwn = args['dbname'].upper()
        dbaddr = os.getenv('MWARE_' + mwn + '_ADDR')
        dbuser = os.getenv('MWARE_' + mwn + '_USER')
        dbpass = os.getenv('MWARE_' + mwn + '_PASS')
        dbname = os.getenv('MWARE_' + mwn + '_DBNAME')
        connstr = 'pq://%s:%s@%s/%s' % (dbuser, dbpass, dbaddr, dbname)
        dbconn = postgresql.open(connstr)
    else:
        print("Reuse connection")
    return dbconn

def main(args):
    db = gcon(args)
    res = "invalid"
    if args['action'] == 'create':
        db.execute("CREATE TABLE data (id SERIAL PRIMARY KEY, key CHAR(64), val CHAR(64))")
        res = "done"
    elif args['action'] == 'insert':
        ins = db.prepare("INSERT INTO data (key, val) VALUES ($1, $2)")
        ins(args['key'], args['val'])
        res = "done"
    elif args['action'] == 'select':
        sel = db.prepare("SELECT val FROM data WHERE key = $1")
        res = sel(args['key'])

    return { "res": res }
