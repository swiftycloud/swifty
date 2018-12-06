BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc

layer.tar: .FORCE
	$(call msg-gen,$@)
	$(Q) $(MKDIR) .layer
	$(Q) $(INST) -D $(BROOT)/swy-wdog -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/swy-runner -t .layer/usr/bin/
	$(Q) $(INST) -D -m 644 $(BROOT)/src/wdog/runner/runner.js -t .layer/home/swifty/
	$(Q) $(INST) runner-js.sh .layer/usr/bin/start_runner.sh
	$(Q) $(TAR) cf layer.tar --xform='s#.layer##' .layer/
	$(Q) $(RM) -rf .layer

swifty/nodejs: layer.tar
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t registry.gitlab.com/swiftyteam/$@ .
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(GITID)
	$(Q) docker tag registry.gitlab.com/swiftyteam/$@ registry.gitlab.com/swiftyteam/$@:$(SWIFTY_ENV)
	$(Q) docker push registry.gitlab.com/swiftyteam/$@
.PHONY: swifty/nodejs

all: swifty/nodejs
	@true
.PHONY: all
