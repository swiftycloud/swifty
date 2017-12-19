#!/usr/bin/env python3

import subprocess
import http.client
import json
import time
import random
import string

def randstr():
	return ''.join(random.choice(string.ascii_letters) for _ in range(0,8))

lext = { 'python' : '.py', 'golang' : '.go' }
swyctl = "./swyctl"

def swyrun(cmdl):
	print(" ".join(cmdl))
	subprocess.check_call([ swyctl ] + cmdl)

def swyrun2(cmdl):
	print(" ".join(cmdl))
	cmd = subprocess.Popen([ swyctl ] + cmdl, stdout = subprocess.PIPE)
	ret = cmd.stdout.readlines()
	cmd.wait()
	return [ i.decode('utf-8') for i in ret ]

def list_fn():
	fns = swyrun2([ 'ls' ])
	return [ i.split()[0].strip() for i in fns[1:] ]

def add_fn(name, lang, mw = [], evt = "url"):
	cmd = [ "add", name, "-lang", lang,
		"-src", "test/functions/" + lang + "/" + name + lext[lang],
		"-event", evt ]
	if mw:
		cmd += [ "-mw", ",".join(mw) ]
	swyrun(cmd)

	return _wait_fn(name)

def upd_fn(inf, lang):
	swyrun([ "upd", inf['name'], "-src", "test/functions/" + lang + "/" + name + "2" + lext[lang] ])

def add_mw(typ, name):
	swyrun([ "madd", name, typ ])

def del_mw(name):
	swyrun([ "mdel", name ])

def _get_inf_fn(name):
	inf = swyrun2([ "inf", name ])
	ret = { i[0].strip(): i[1].strip() for i in [ i.split(':', 1) for i in inf ] }
	ret["name"] = name
	return ret

def inf_fn(inf):
	return _get_inf_fn(inf['name'])

def _wait_fn(name):
	tmo = 1
	while True:
		time.sleep(tmo)
		inf = _get_inf_fn(name)
		if inf['State'] == 'ready':
			return inf
		tmo *= 2

def run_fn(inf, args):
	url = inf['URL'].split('/', 3)
	conn = http.client.HTTPConnection(url[2])
	conn.request('GET', '/' + url[3] + '?' + '&'.join([x[0]+'='+x[1] for x in args.items()]))
	resp = conn.getresponse()
	rv = resp.read()
	print("Returned: [%s]" % rv)
	return json.loads(rv)

def del_fn(inf):
	swyrun([ "logs", inf['name'] ])
	swyrun([ "del", inf['name'] ])


def run_test(fname, langs):
	print("====== Running %s" % fname.__name__)
	for l in langs:
		print("______ %s" % l)
		if fname(l):
			print("------ PASS")
		else:
			print("====== FAIL")

def helloworld(lang):
	cookie = randstr()
	inf = add_fn("helloworld", lang)
	ret = run_fn(inf, {'name': cookie})
	del_fn(inf)
	print(ret)
	return ret['message'] == 'hw:%s:%s' % (lang, cookie)

def update(lang):
	ok = False
	cookie = randstr()
	inf = add_fn("helloworld", lang)
	ret = run_fn(inf, {'name': cookie})
	print(ret)
	if ret['message'] == 'hw:%s:%s' % (lang, cookie):
		upd_fn(inf, lang)
		tmo = 0.5
		while tmo < 12.0:
			ret = run_fn(inf, {'name': cookie})
			print(ret)
			if ret['message'] == 'hw:%s:%s' % (lang, cookie):
				print("Updating")
				time.sleep(tmo)
				tmo *= 2.0
				continue
			if ret['message'] == 'hw2:%s:%s' % (lang, cookie):
				ok = True
				print("Updated")
			else:
				print("Alien message")
			break
	del_fn(inf)
	return ok

def pgsql(lang):
	ok = False
	dbname = 'pgtst'
	cookie = randstr()
	args_c = { 'dbname': dbname, 'action': 'create' }
	args_i = { 'dbname': dbname, 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'action': 'select', 'key': 'foo' }


	add_mw("postgres", dbname)
	inf = add_fn("pgsql", lang, mw = [ dbname ])
	ret = run_fn(inf, args_c)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_i)
		print(ret)
		if ret.get('res', '') == 'done':
			ret = run_fn(inf, args_s)
			print(ret)
			if ret.get('res', [['']])[0][0].strip() == cookie:
				ok = True
	del_fn(inf)
	del_mw(dbname)
	return ok

def mongo(lang):
	ok = False
	dbname = 'mgotst'
	cookie = randstr()
	args_i = { 'dbname': dbname, 'collection': 'tcol', 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'collection': 'tcol', 'action': 'select', 'key': 'foo' }

	add_mw("mongo", dbname)
	inf = add_fn("mongo", lang, mw = [ dbname ])
	ret = run_fn(inf, args_i)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_s)
		print(ret)
		if ret.get('res', '') == cookie:
			ok = True
	del_fn(inf)
	del_mw(dbname)
	return ok

def s3(lang):
	ok = False
	s3name = 's3tns'
	cookie = randstr()
	args_c = { 's3name': s3name, 'action': 'create', 'bucket': 'tbuck' }
	args_p = { 's3name': s3name, 'action': 'put',    'bucket': 'tbuck' , 'name': 'tobj', 'data': cookie }
	args_g = { 's3name': s3name, 'action': 'get',    'bucket': 'tbuck' , 'name': 'tobj' }

	add_mw("s3", s3name)
	inf = add_fn("s3", lang, mw = [ s3name ])
	ret = run_fn(inf, args_c)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_p)
		print(ret)
		if ret.get('res', '') == 'done':
			ret = run_fn(inf, args_g)
			print(ret)
			if ret.get('res', '') == cookie:
				ok = True
	del_fn(inf)
	del_mw(s3name)
	return ok

def s3notify(lang):
	ok = False
	s3name = 's3nns'
	args_c = { 's3name': s3name, 'action': 'create', 'bucket': 'tbuck' }
	args_p = { 's3name': s3name, 'action': 'put',    'bucket': 'tbuck' , 'name': 'xobj', 'data': 'abc' }

	add_mw("s3", s3name)
	inf = add_fn("s3", lang, mw = [ s3name ])
	ret = run_fn(inf, args_c)
	print(ret)

	if ret.get('res', '') == 'done':
		infe = add_fn("s3evt", lang, evt = 'mware:%s:b=tbuck' % s3name)
		ret = run_fn(inf, args_p)
		print(ret)
		if ret.get('res', '') == 'done':
			infe = inf_fn(infe)
			if infe['Called'] == '0':
				ok = True
		del_fn(infe)

	del_fn(inf)
	del_mw(s3name)
	return ok

def checkempty(lang):
	fns = list_fn()
	print(fns)
	return len(fns) == 0


#run_test(helloworld, ["python", "golang"])
#run_test(update, ["python"])
#run_test(pgsql, ["python"])
#run_test(mongo, ["python"])
#run_test(s3, ["python"])
run_test(s3notify, ["python"])
run_test(checkempty, [""])
