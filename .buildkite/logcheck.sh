#!/bin/sh
# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#      http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

if [ -z "$BUILDKITE_PULL_REQUEST_BASE_BRANCH" ]; then
    # only run on pull requests
    exit 0
fi

found_fixups=0
missing_dco=0

for sha in $(git log --no-merges --pretty=%H --no-decorate \
             "${BUILDKITE_PULL_REQUEST_BASE_BRANCH}"..HEAD); do
    git show --pretty=oneline --no-decorate --no-patch $sha \
      | fgrep -q 'fixup!' && {
        found_fixups=1
        echo "Found fixup commit $sha"
    }

    git show --pretty=medium --no-decorate --no-patch $sha \
      | fgrep -q Signed-off-by: || {
        missing_dco=1
        echo "Missing DCO on $sha"
    }
done

status=0
if [ $found_fixups -gt 0 ]; then
    status=1
else
    echo "Found no fixup commits"
fi

if [ $missing_dco -gt 0 ]; then
    status=1
else
    echo "All commits have Signed-off-by signature"
fi

exit $status
