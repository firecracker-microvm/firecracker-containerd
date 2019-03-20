#!/usr/bin/env bash
set -e

DOCKER_RUN_ARGS="$1"
DOCKER_IMAGE="$2"

# test_names returns (via its stdout) a list of test names that match the provided regular expression
test_names () {
	TEST_NAME_REGEX="$1"
	DOCKER_IMAGE="$2"

	docker run --rm \
		--env FCCD_PACKAGE_DIR="${FCCD_PACKAGE_DIR}" \
		"${DOCKER_IMAGE}" \
		'
			cd /firecracker-containerd/${FCCD_PACKAGE_DIR}
			go test -list .
		' | sed '$d' | grep "${TEST_NAME_REGEX}"
}

# For each test case with a name matching the provided regex, run each one in its own isolated docker container
for TESTNAME in $(test_names "${FCCD_TESTNAME_REGEX}" "${DOCKER_IMAGE}"); do
	docker run --rm \
		--env TESTNAME="${TESTNAME}" \
		--env FCCD_PACKAGE_DIR="${FCCD_PACKAGE_DIR}" \
		${DOCKER_RUN_ARGS} \
		"${DOCKER_IMAGE}" \
		'
			echo "FCCD_PACKAGE_DIR = ${FCCD_PACKAGE_DIR}"
			echo "TESTNAME = ${TESTNAME}"
			cd /firecracker-containerd/${FCCD_PACKAGE_DIR}
			go test -v -count=1 -run "^${TESTNAME}$" .
		'
done
