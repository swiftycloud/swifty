import pymysql.cursors
import os
import swifty

def main(req):
    db = swifty.MariaConn(req.args['dbname'])
    res = "invalid"
    with db.cursor() as cursor:
        if req.args['action'] == 'create':
            cursor.execute("CREATE TABLE `data` (`key` VARCHAR(64), `val` VARCHAR(64))")
            res = "done"
        elif req.args['action'] == 'insert':
            cursor.execute("INSERT INTO `data` (`key`, `val`) VALUES (%s, %s)", (req.args['key'], req.args['val']))
            res = "done"
        elif req.args['action'] == 'select':
            cursor.execute("SELECT `val` FROM `data` WHERE `key` = %s", (req.args['key'],))
            res = cursor.fetchone()['val']
    db.commit()
    return { "res": res }, None
