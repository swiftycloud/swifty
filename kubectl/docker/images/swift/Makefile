swifty/swift:
	$(call msg-gen,$@)
	$(Q) docker build -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/swift

all: swifty/swift
	@true
.PHONY: all
