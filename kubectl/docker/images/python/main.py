# Gate puts downloaded sources into /function/code/
# and for single file it's name is script.py
import sys
import json
from code import script

ret = script.main(json.loads(sys.argv[1]))
with open("/dev/shm/swyresult.json", 'w') as outf:
    json.dump(ret, outf)
