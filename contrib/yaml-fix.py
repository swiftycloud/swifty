#!/usr/bin/env python3

import yaml
import sys

# usage -- kt 'input' 'input:file'

source = yaml.load(open(sys.argv[1]))

for fixes in sys.argv[2:]:
	s = fixes.split(':')
	print("Fix %s from %s" % (s[0], s[1]))

	path = s[0].split('.')
	ln = path[-1]
	x = source
	for k in path[:-1]:
		try:
			i = int(k)
			k = i
		except:
			pass
		x = x[k]

	fix = yaml.load(open(s[1]))
	x[ln] = fix[ln]

print(yaml.dump(source, default_flow_style=False))
