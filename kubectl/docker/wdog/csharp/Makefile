IMAGE="swifty/csharp"
BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc
include $(BROOT)/kubectl/docker/Makefile.inc

layer.tar: .FORCE
	$(call msg-gen,$@)
	$(Q) $(MKDIR) .layer
	$(Q) $(INST) -D $(BROOT)/swy-wdog -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/swy-runner -t .layer/usr/bin/
	$(Q) $(INST) -D -m 644 $(BROOT)/src/wdog/runner/runner.cs -t .layer/mono/runner/
	$(Q) $(INST) -m 644 $(BROOT)/src/wdog/lib/XStream.dll .layer/mono/runner/
	$(Q) $(INST) runner-mono.sh .layer/usr/bin/start_runner.sh
	$(Q) $(INST) builder.sh .layer/usr/bin/build_runner.sh
	$(Q) $(TAR) cf layer.tar --xform='s#.layer##' .layer/
	$(Q) $(RM) -rf .layer

$(eval $(call gen-docker-targets,$(IMAGE)))

all: image push
	@true
.PHONY: all
