#!/usr/bin/env bash
set -e

# test_names returns (via its stdout) a list of test names that match the provided regular expression
test_names () {
    docker run --rm \
           --workdir="/firecracker-containerd/${FCCD_PACKAGE_DIR}" \
           "${FCCD_DOCKER_IMAGE}" \
           "go test -list ." | sed '$d' | grep "${FCCD_TESTNAME_REGEX}"
}

# For each test case with a name matching the provided regex, run each one in its own isolated docker container
for TESTNAME in $(test_names); do
    echo "TESTNAME = ${TESTNAME}"
    docker run --rm \
           --workdir="/firecracker-containerd/${FCCD_PACKAGE_DIR}" \
           --env TESTNAME="${TESTNAME}" \
		       --env EXTRAGOARGS="${EXTRAGOARGS}" \
           ${FCCD_DOCKER_RUN_ARGS} \
           "${FCCD_DOCKER_IMAGE}" \
           'go test ${EXTRAGOARGS} -run "^${TESTNAME}$" .'
done
