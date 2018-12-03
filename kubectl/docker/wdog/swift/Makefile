BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc

layer.tar: .FORCE
	$(Q) $(MKDIR) .layer
	$(Q) $(INST) -D $(BROOT)/swy-wdog -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/swy-runner -t .layer/usr/bin/
	$(Q) $(INST) -D -m 0644 $(BROOT)/src/wdog/runner/runner.swift .layer/swift/runner/Sources/main.swift
	$(Q) $(INST) -m 0644 Package.swift -t .layer/swift/runner/
	$(Q) $(INST) runner-swift.sh .layer/usr/bin/start_runner.sh
	$(Q) $(INST) builder.sh .layer/usr/bin/build_runner.sh
	$(Q) $(TAR) cf layer.tar --xform='s#.layer##' .layer/
	$(Q) $(RM) -rf .layer

swifty/swift: layer.tar
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/swift

all: swifty/swift
	@true
.PHONY: all
