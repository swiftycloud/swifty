# Gate puts downloaded sources into /function/code/
# and for single file it's name is script.py
import sys
from code import script
fn = getattr(script, sys.argv[1])
fn(**eval(sys.argv[2]))
