swifty/ruby:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/ruby

all: swifty/ruby
	@true
.PHONY: all
