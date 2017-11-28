#!/usr/bin/env python3

import subprocess
import http.client
import json
import time
import random
import string

def randstr():
	return ''.join(random.choice(string.ascii_letters) for _ in range(0,8))

lext = { 'python' : '.py' }
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

def add_fn(name, lang, mw = []):
	cmd = [ "add", name, "-lang", lang,
		"-src", "test/functions/" + lang + "/" + name + lext[lang],
		"-event", "url" ]
	if mw:
		cmd += [ "-mw", ",".join(mw) ]
	swyrun(cmd)

        return wait_fn(name)

def add_mw(typ, name):
	swyrun([ "madd", name, typ ])

def del_mw(name):
	swyrun([ "mdel", name ])

def get_inf_fn(name):
	inf = swyrun2([ "inf", name ])
	return { i[0].strip(): i[1].strip() for i in [ i.split(':', 1) for i in inf ] }

def wait_fn(name):
	tmo = 1
	while True:
		time.sleep(tmo)
		inf = get_inf_fn(name)
		if inf['State'] == 'ready':
			return inf
		tmo *= 2

def run_fn(inf, args):
	url = inf['URL'].split('/', 3)
	conn = http.client.HTTPConnection(url[2])
	conn.request('GET', '/' + url[3] + '?' + '&'.join([x[0]+'='+x[1] for x in args.items()]))
	resp = conn.getresponse()
	return json.loads(resp.read())

def del_fn(name):
	swyrun([ "del", name ])

def run_test(fname):
	print("====== Running %s" % fname.__name__)
	if fname():
		print("------ PASS")
	else:
		print("====== FAIL")

def helloworld():
	inf = add_fn("helloworld", "python")
	ret = run_fn(inf, {'name': 'foo'})
	del_fn("helloworld")
	print(ret)
	return ret['message'] == 'hw:python:foo'

def pgsql():
	ok = False
	dbname = 'pgtst'
	cookie = randstr()
	args_c = { 'dbname': dbname, 'action': 'create' }
	args_i = { 'dbname': dbname, 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'action': 'select', 'key': 'foo' }


	add_mw("postgres", dbname)
	inf = add_fn("pgsql", "python", mw = [ dbname ])
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
	del_fn("pgsql")
	del_mw(dbname)
	return ok

def mongo():
	ok = False
	dbname = 'mgotst'
	cookie = randstr()
	args_i = { 'dbname': dbname, 'collection': 'tcol', 'action': 'insert', 'key': 'foo', 'val': cookie }
	args_s = { 'dbname': dbname, 'collection': 'tcol', 'action': 'select', 'key': 'foo' }

	add_mw("mongo", dbname)
	inf = add_fn("mongo", "python", mw = [ dbname ])
	ret = run_fn(inf, args_i)
	print(ret)
	if ret.get('res', '') == 'done':
		ret = run_fn(inf, args_s)
		print(ret)
		if ret.get('res', '') == cookie:
			ok = True
	del_fn("mongo")
	del_mw(dbname)
	return ok

def checkempty():
	fns = list_fn()
	print(fns)
	return len(fns) == 0


#run_test(helloworld)
#run_test(pgsql)
#run_test(mongo)
run_test(checkempty)
