swifty/csharp:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/csharp

all: swifty/csharp
	@true
.PHONY: all
