import pymysql.cursors
import os
import swifty

def main(args):
    db = swifty.MariaConn(args['dbname'])
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
