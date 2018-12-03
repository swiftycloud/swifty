BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc

layer.tar: .FORCE
	$(Q) $(MKDIR) .layer
	$(Q) $(INST) -D $(BROOT)/swy-wdog -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/swy-runner -t .layer/usr/bin/
	$(Q) $(INST) -D -m 644 $(BROOT)/src/wdog/runner/runner.cs -t .layer/mono/runner/
	$(Q) $(INST) -m 644 $(BROOT)/src/wdog/lib/XStream.dll .layer/mono/runner/
	$(Q) $(INST) runner-mono.sh .layer/usr/bin/start_runner.sh
	$(Q) $(INST) builder.sh .layer/usr/bin/build_runner.sh
	$(Q) $(TAR) cf layer.tar --xform='s#.layer##' .layer/
	$(Q) $(RM) -rf .layer


swifty/csharp: layer.tar
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/csharp

all: swifty/csharp
	@true
.PHONY: all
