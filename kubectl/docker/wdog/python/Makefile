BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc

layer.tar: .FORCE
	$(call msg-gen,$@)
	$(Q) $(MKDIR) .layer
	$(Q) $(INST) -D $(BROOT)/swy-wdog -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/swy-runner -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/src/wdog/runner/runner.py .layer/usr/bin/swy-runner.py
	$(Q) $(INST) -m 0644 -D $(BROOT)/src/wdog/lib/lib.py .layer/swifty/swifty.py
	$(Q) $(INST) runner-py.sh .layer/usr/bin/start_runner.sh
	$(Q) $(INST) xpip.py -t .layer/usr/bin/
	$(Q) $(TAR) cf layer.tar --xform='s#.layer##' .layer/
	$(Q) $(RM) -rf .layer

swifty/python: layer.tar
	$(call msg-gen,$@)
	$(Q) docker build -t registry.gitlab.com/swiftyteam/$@ .
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(GITID)
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(SWIFTY_ENV)
	$(Q) docker push registry.gitlab.com/swiftyteam/$@
.PHONY: swifty/python

all: swifty/python
	@true
.PHONY: all
