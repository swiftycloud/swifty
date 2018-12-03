BROOT=$(CURDIR)/../../../../

swifty/python:
	$(call msg-gen,$@)
	$(Q) $(CP) $(BROOT)/swy-wdog .
	$(Q) $(CP) $(BROOT)/swy-runner .
	$(Q) $(CP) $(BROOT)/src/wdog/runner/runner.py ./swy-runner.py
	$(Q) $(CP) $(BROOT)/src/wdog/lib/lib.py ./swifty.py
	$(Q) docker build -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/python

all: swifty/python
	@true
.PHONY: all
