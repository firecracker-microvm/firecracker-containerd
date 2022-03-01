#! /bin/bash
#
# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
# 	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.


# Create temporary critest output file
critest_output="$(</dev/stdin)"
critest_output_file="$(mktemp)"
echo "$critest_output" >> "$critest_output_file"

set -eu

# Remove up until report summary
sed -i -E '0,/^Summarizing [0-9][0-9]? Failure[s]?:$/d' "$critest_output_file" # Remove empty lines
sed -i '/^$/d' "$critest_output_file"

# Remove unnecessary error messages
sed -i '/^\/.*[0-9]$/d' "$critest_output_file"
sed -i '/^Ran [0-9][0-9] of [0-9][0-9] Specs in .*seconds$/d' "$critest_output_file"
sed -i '/^--- FAIL: TestCRISuite.*$/d' "$critest_output_file"
sed -i '/^FAIL.*$/d' "$critest_output_file"
sed -i '/^Ran.*$/d' "$critest_output_file"

# Diff expected vs. actual
diff -y <(sort critest/expected_critest_output.out) <(sort "$critest_output_file")