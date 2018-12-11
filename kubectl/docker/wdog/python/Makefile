IMAGE="swifty/python"
BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc
include $(BROOT)/kubectl/docker/Makefile.inc

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

$(eval $(call gen-docker-targets,$(IMAGE)))

all: image push
	@true
.PHONY: all
