TESTS =

TESTS += test-fns

all: $(TESTS)
	true

test-%: test-%.go
	go build $<
