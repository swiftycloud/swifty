import pymysql.cursors
import os

dbconn = None

def gcon(args):
    global dbconn
    if dbconn == None:
        print("New connection")
        mwn = args['dbname'].upper()
        x = os.getenv('MWARE_' + mwn + '_ADDR').split(":")
        dbaddr = x[0]
        dbport = int(x[1])
        dbuser = os.getenv('MWARE_' + mwn + '_USER')
        dbpass = os.getenv('MWARE_' + mwn + '_PASS')
        dbname = os.getenv('MWARE_' + mwn + '_DBNAME')
        print("Connect to %s:%s@%s:%d/%s" % (dbuser, dbpass, dbaddr, dbport, dbname))
        dbconn = pymysql.connect(host=dbaddr, port=dbport,
                        user=dbuser, password=dbpass, db=dbname,
                        cursorclass=pymysql.cursors.DictCursor)
    else:
        print("Reuse connection")
    return dbconn

def main(args):
    db = gcon(args)
    res = "invalid"
    with db.cursor() as cursor:
        if args['action'] == 'create':
            cursor.execute("CREATE TABLE `data` (`key` VARCHAR(64), `val` VARCHAR(64))")
            res = "done"
        elif args['action'] == 'insert':
            cursor.execute("INSERT INTO `data` (`key`, `val`) VALUES (%s, %s)", (args['key'], args['val']))
            res = "done"
        elif args['action'] == 'select':
            cursor.execute("SELECT `val` FROM `data` WHERE `key` = %s", (args['key'],))
            res = cursor.fetchone()['val']
    db.commit()
    return { "res": res }
