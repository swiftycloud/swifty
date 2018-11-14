/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

#define _GNU_SOURCE
#include <unistd.h>
#include <stdlib.h>
#include <sys/fsuid.h>

#define NOBODY	99

int main(int argc, char **argv)
{
	int fd;

	/* Usage: runner outfd errfd queuefd cmd <args> */
	fd = atoi(argv[1]);
	if (fd >= 0) {
		if (fd <= 2)
			exit(1);
		dup2(fd, 1);
		close(fd);
	}

	if (fd >= 0) {
		fd = atoi(argv[2]);
		if (fd <= 2)
			exit(1);
		dup2(fd, 2);
		close(fd);
	}

	fd = atoi(argv[3]);
	if (fd != 3) {
		dup2(fd, 3);
		close(fd);
	}

	setresgid(NOBODY, NOBODY, NOBODY);
	setfsgid(NOBODY);
	setresuid(NOBODY, NOBODY, NOBODY);
	setfsuid(NOBODY);

	execv(argv[4], argv + 4);
	exit(1);
}
