swifty/swift:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/swift

all: swifty/swift
	@true
.PHONY: all
