ifeq ($(strip $(V)),)
        E := @echo
        Q := @
else
        E := @\#
        Q :=
endif

export E Q

define msg-gen
        $(E) "  GEN     " $(1)
endef

define msg-clean
        $(E) "  CLEAN   " $(1)
endef

export msg-gen msg-clean

MAKEFLAGS += --no-print-directory
export MAKEFLAGS

RM		?= rm -f
MAKE		?= make
GIT		?= git
CP		?= cp -f
MV		?= mv -f
GO		?= go
GO-BUILD-OPTS	?= build
GOTAGS		?= gotags
MONGO		?= mongo --quiet localhost:27017
KUBECTL		?= kubectl
IPVSADM		?= ipvsadm
FLAVOR		?= default

LOCAL_SOURCES	?= /home/swifty/local-sources
VOLUME_DIR	?= /home/swifty-volume
TEST_REPO	?= test/.repo

export RM MAKE GIT CP GO GO-BUILD-OPTS GOTAGS MONGO KUBECTL IPVSADM
