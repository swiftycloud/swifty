IMG_PREFIX ?= "registry.gitlab.com/swiftyteam/"

define gen-docker-targets
image: layer.tar
	$$(call msg-gen,$(1))
	$$(Q) docker build --network=host -t $$(IMG_PREFIX)$(1) .
	$$(Q) docker tag $$(IMG_PREFIX)$(1) $$(IMG_PREFIX)$(1):$$(GITID)
	$$(Q) docker tag $$(IMG_PREFIX)$(1) $$(IMG_PREFIX)$(1):$$(SWIFTY_ENV)
.PHONY: image

push: image
	$$(call msg-gen,$$@)
	$$(Q) docker push $$(IMG_PREFIX)$(1)
.PHONY: push
endef
